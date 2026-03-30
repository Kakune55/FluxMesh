package traffic

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"fluxmesh/internal/model"
)

type balancerStub struct {
	addr string
}

func (b *balancerStub) Pick(_ string, _ model.BackendGroup, _ *planState) (string, error) {
	return b.addr, nil
}

func TestNormalizeLBStrategy(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "default when empty", input: "", expected: "load-first"},
		{name: "round robin alias", input: " rr ", expected: "round-robin"},
		{name: "random alias", input: "rand", expected: "random"},
		{name: "latency-first normalized", input: " LATENCY-FIRST ", expected: "latency-first"},
		{name: "unknown strategy passthrough", input: " custom-plug ", expected: "custom-plug"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeLBStrategy(tc.input)
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestRegisterBalancerValidationAndDuplicate(t *testing.T) {
	if err := RegisterBalancer("", &balancerStub{addr: "127.0.0.1:1"}); err == nil {
		t.Fatalf("expected empty name validation error")
	}
	if err := RegisterBalancer("test-nil-impl", nil); err == nil {
		t.Fatalf("expected nil balancer validation error")
	}

	name := "test-balancer-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := RegisterBalancer(name, &balancerStub{addr: "127.0.0.1:29991"}); err != nil {
		t.Fatalf("register balancer failed: %v", err)
	}
	if err := RegisterBalancer(name, &balancerStub{addr: "127.0.0.1:29992"}); err == nil {
		t.Fatalf("expected duplicate registration error")
	}
}

func TestSelectBackendTargetErrorsAndSuccess(t *testing.T) {
	plan := Plan{state: newPlanState()}

	_, err := plan.selectBackendTarget("empty", model.BackendGroup{Name: "empty"}, "round-robin")
	if err == nil {
		t.Fatalf("expected no-target error")
	}

	group := model.BackendGroup{
		Name: "g1",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28081", Weight: 1},
		},
	}
	_, err = plan.selectBackendTarget("g1", group, "unknown-strategy")
	if err == nil {
		t.Fatalf("expected unregistered strategy error")
	}

	got, err := plan.selectBackendTarget("g1", group, "round-robin")
	if err != nil {
		t.Fatalf("unexpected select error: %v", err)
	}
	if got != "127.0.0.1:28081" {
		t.Fatalf("expected 127.0.0.1:28081, got %s", got)
	}
}

func TestNextRoundRobinIndexSequenceAndDegenerate(t *testing.T) {
	state := &planState{
		rrCounters: map[string]uint64{},
		rng:        rand.New(rand.NewSource(1)),
	}

	if got := nextRoundRobinIndex("g", 2, state); got != 0 {
		t.Fatalf("expected first index 0, got %d", got)
	}
	if got := nextRoundRobinIndex("g", 2, state); got != 1 {
		t.Fatalf("expected second index 1, got %d", got)
	}
	if got := nextRoundRobinIndex("g", 2, state); got != 0 {
		t.Fatalf("expected third index 0, got %d", got)
	}

	if got := nextRoundRobinIndex("g", 2, nil); got != 0 {
		t.Fatalf("expected nil-state index 0, got %d", got)
	}
	if got := nextRoundRobinIndex("g", 1, state); got != 0 {
		t.Fatalf("expected single-target index 0, got %d", got)
	}
}

func TestWeightedRandomTarget(t *testing.T) {
	one := model.BackendGroup{
		Name: "single",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28081", Weight: 100},
		},
	}
	got, err := weightedRandomTarget(one, nil)
	if err != nil {
		t.Fatalf("single-target random returned error: %v", err)
	}
	if got != "127.0.0.1:28081" {
		t.Fatalf("expected single target addr, got %s", got)
	}

	invalid := model.BackendGroup{
		Name: "invalid",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28082", Weight: 0},
			{Addr: "127.0.0.1:28083", Weight: 0},
		},
	}
	if _, err := weightedRandomTarget(invalid, nil); err == nil {
		t.Fatalf("expected invalid total weight error")
	}

	multi := model.BackendGroup{
		Name: "multi",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28084", Weight: 1},
			{Addr: "127.0.0.1:28085", Weight: 9},
		},
	}
	state := &planState{
		rrCounters: map[string]uint64{},
		rng:        rand.New(rand.NewSource(7)),
	}
	for i := 0; i < 20; i++ {
		addr, err := weightedRandomTarget(multi, state)
		if err != nil {
			t.Fatalf("weighted random failed: %v", err)
		}
		if addr != "127.0.0.1:28084" && addr != "127.0.0.1:28085" {
			t.Fatalf("unexpected weighted random addr: %s", addr)
		}
	}
}

func TestLoadFirstBalancerPick(t *testing.T) {
	b := &loadFirstBalancer{}

	group := model.BackendGroup{
		Name: "pick-max",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28081", Weight: 10},
			{Addr: "127.0.0.1:28082", Weight: 100},
			{Addr: "127.0.0.1:28083", Weight: 20},
		},
	}
	got, err := b.Pick("g", group, nil)
	if err != nil {
		t.Fatalf("load-first pick failed: %v", err)
	}
	if got != "127.0.0.1:28082" {
		t.Fatalf("expected max-weight addr 127.0.0.1:28082, got %s", got)
	}

	tie := model.BackendGroup{
		Name: "tie",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28090", Weight: 100},
			{Addr: "127.0.0.1:28089", Weight: 100},
		},
	}
	got, err = b.Pick("g", tie, nil)
	if err != nil {
		t.Fatalf("load-first tie pick failed: %v", err)
	}
	if got != "127.0.0.1:28089" {
		t.Fatalf("expected lexicographically smaller addr on tie, got %s", got)
	}
}

func TestLatencyFirstBalancerPrefersObservedLatency(t *testing.T) {
	b := &latencyFirstBalancer{}
	state := newPlanState()

	state.ObserveLatency("127.0.0.1:28081", 40*time.Millisecond)
	state.ObserveLatency("127.0.0.1:28082", 5*time.Millisecond)

	group := model.BackendGroup{
		Name: "latency-pick",
		Targets: []model.BackendTarget{
			{Addr: "127.0.0.1:28081", Weight: 100},
			{Addr: "127.0.0.1:28082", Weight: 10},
		},
	}

	got, err := b.Pick("latency-pick", group, state)
	if err != nil {
		t.Fatalf("latency-first pick failed: %v", err)
	}
	if got != "127.0.0.1:28082" {
		t.Fatalf("expected lower-latency target 127.0.0.1:28082, got %s", got)
	}
}
