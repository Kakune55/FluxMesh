package traffic

import (
	"testing"

	"fluxmesh/internal/model"
)

type fixedAddrBalancer struct {
	addr string
}

func (b *fixedAddrBalancer) Pick(_ string, _ model.BackendGroup, _ *planState) (string, error) {
	return b.addr, nil
}

func TestBuildPlanAndMatch(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "payment-v1",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28081", Weight: 100},
					},
				},
			},
			Routes: []model.ServiceRoute{
				{Hosts: []string{"pay.example.com"}, PathPrefix: "/", Destination: "payment-v1", Weight: 100},
			},
		},
		{
			Name: "order-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "order-v2",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28082", Weight: 100},
					},
				},
				{
					Name: "order-fallback",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28083", Weight: 50},
						{Addr: "127.0.0.1:28084", Weight: 100},
					},
				},
			},
			Routes: []model.ServiceRoute{
				{Hosts: []string{"api.example.com"}, PathPrefix: "/orders", Destination: "order-v2", Weight: 100},
				{Hosts: []string{"*"}, PathPrefix: "/", Destination: "order-fallback", Weight: 50},
			},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	listeners := plan.Listeners()
	if len(listeners) != 1 {
		t.Fatalf("expected 1 merged listener, got %d", len(listeners))
	}
	if listeners[0].Listener.Port != 18080 {
		t.Fatalf("expected listener port 18080, got %d", listeners[0].Listener.Port)
	}
	if len(listeners[0].Routes) != 3 {
		t.Fatalf("expected 3 routes in merged listener, got %d", len(listeners[0].Routes))
	}

	result, ok := plan.Match("0.0.0.0", 18080, "api.example.com", "/orders/123")
	if !ok {
		t.Fatalf("expected route match")
	}
	if result.ServiceName != "order-svc" || result.Destination != "order-v2" {
		t.Fatalf("unexpected match result: %+v", result)
	}

	result, ok = plan.Match("0.0.0.0", 18080, "pay.example.com:18080", "/checkout")
	if !ok {
		t.Fatalf("expected route match by host without port sensitivity")
	}
	if result.ServiceName != "payment-svc" {
		t.Fatalf("expected payment-svc, got %s", result.ServiceName)
	}

	result, ok = plan.Match("0.0.0.0", 18080, "unknown.example.com", "/anything")
	if !ok {
		t.Fatalf("expected wildcard fallback route")
	}
	if result.Destination != "order-fallback" {
		t.Fatalf("expected order-fallback, got %s", result.Destination)
	}

	resolved, err := plan.ResolveDestination(result.Destination)
	if err != nil {
		t.Fatalf("resolve destination failed: %v", err)
	}
	if resolved != "127.0.0.1:28084" {
		t.Fatalf("expected highest-weight backend target 127.0.0.1:28084, got %s", resolved)
	}
}

func TestBuildPlanInvalidService(t *testing.T) {
	services := []model.ServiceConfig{{
		Name:          "invalid-svc",
		TrafficPolicy: model.ServiceTrafficPolicy{Listener: model.ListenerPolicy{Port: 0}},
		Routes:        []model.ServiceRoute{{PathPrefix: "/", Destination: "x", Weight: 100}},
	}}

	_, err := BuildPlan(services)
	if err == nil {
		t.Fatalf("expected build plan validation error")
	}
}

func TestResolveDestinationServiceFallback(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "upstream-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 19080},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:28090", Weight: 100}},
		},
		{
			Name: "gateway-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "upstream-svc", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	resolved, err := plan.ResolveDestination("upstream-svc")
	if err != nil {
		t.Fatalf("resolve service destination failed: %v", err)
	}
	if resolved != "127.0.0.1:19080" {
		t.Fatalf("expected service listener fallback 127.0.0.1:19080, got %s", resolved)
	}
}

func TestResolveDestinationDirectAndUnresolved(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "gateway-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "127.0.0.1", Port: 18080},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:28090", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	resolved, err := plan.ResolveDestination("https://example.com:443")
	if err != nil {
		t.Fatalf("resolve direct url failed: %v", err)
	}
	if resolved != "https://example.com:443" {
		t.Fatalf("expected unchanged direct url, got %s", resolved)
	}

	resolved, err = plan.ResolveDestination("127.0.0.1:28090")
	if err != nil {
		t.Fatalf("resolve direct host:port failed: %v", err)
	}
	if resolved != "127.0.0.1:28090" {
		t.Fatalf("expected unchanged direct host:port, got %s", resolved)
	}

	_, err = plan.ResolveDestination("unknown-destination")
	if err == nil {
		t.Fatalf("expected unresolved destination error")
	}
}

func TestBuildPlanDuplicateBackendGroupName(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "svc-a",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
			},
			BackendGroups: []model.BackendGroup{{
				Name:    "shared",
				Targets: []model.BackendTarget{{Addr: "127.0.0.1:28081", Weight: 100}},
			}},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "shared", Weight: 100}},
		},
		{
			Name: "svc-b",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18081},
			},
			BackendGroups: []model.BackendGroup{{
				Name:    "shared",
				Targets: []model.BackendTarget{{Addr: "127.0.0.1:28082", Weight: 100}},
			}},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "shared", Weight: 100}},
		},
	}

	_, err := BuildPlan(services)
	if err == nil {
		t.Fatalf("expected duplicate backend group build error")
	}
}

func TestResolveDestinationRoundRobinStrategy(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "round-robin"},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "payment-v1",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28081", Weight: 100},
						{Addr: "127.0.0.1:28082", Weight: 100},
					},
				},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	first, err := plan.ResolveDestination("payment-v1")
	if err != nil {
		t.Fatalf("resolve first failed: %v", err)
	}
	second, err := plan.ResolveDestination("payment-v1")
	if err != nil {
		t.Fatalf("resolve second failed: %v", err)
	}
	third, err := plan.ResolveDestination("payment-v1")
	if err != nil {
		t.Fatalf("resolve third failed: %v", err)
	}

	if first != "127.0.0.1:28081" || second != "127.0.0.1:28082" || third != "127.0.0.1:28081" {
		t.Fatalf("unexpected round-robin sequence: %s, %s, %s", first, second, third)
	}
}

func TestResolveDestinationRandomStrategy(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "random"},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "payment-v1",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28081", Weight: 1},
						{Addr: "127.0.0.1:28082", Weight: 9},
					},
				},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		resolved, err := plan.ResolveDestination("payment-v1")
		if err != nil {
			t.Fatalf("resolve random failed: %v", err)
		}
		if resolved != "127.0.0.1:28081" && resolved != "127.0.0.1:28082" {
			t.Fatalf("unexpected random resolved target: %s", resolved)
		}
	}
}

func TestResolveDestinationLatencyFirstStrategy(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "latency-first"},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "payment-v1",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28081", Weight: 10},
						{Addr: "127.0.0.1:28082", Weight: 100},
					},
				},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	resolved, err := plan.ResolveDestination("payment-v1")
	if err != nil {
		t.Fatalf("resolve latency-first failed: %v", err)
	}
	if resolved != "127.0.0.1:28082" {
		t.Fatalf("expected highest-priority target 127.0.0.1:28082, got %s", resolved)
	}
}

func TestResolveDestinationUnregisteredCustomStrategy(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "custom-plug"},
			},
			BackendGroups: []model.BackendGroup{
				{Name: "payment-v1", Targets: []model.BackendTarget{{Addr: "127.0.0.1:28081", Weight: 100}}},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	_, err = plan.ResolveDestination("payment-v1")
	if err == nil {
		t.Fatalf("expected unregistered balancer error")
	}
}

func TestResolveDestinationRegisteredCustomStrategy(t *testing.T) {
	if err := RegisterBalancer("custom-plug", &fixedAddrBalancer{addr: "127.0.0.1:29999"}); err != nil {
		t.Fatalf("register custom balancer failed: %v", err)
	}

	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "custom-plug"},
			},
			BackendGroups: []model.BackendGroup{
				{Name: "payment-v1", Targets: []model.BackendTarget{{Addr: "127.0.0.1:28081", Weight: 100}}},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	resolved, err := plan.ResolveDestination("payment-v1")
	if err != nil {
		t.Fatalf("resolve custom strategy failed: %v", err)
	}
	if resolved != "127.0.0.1:29999" {
		t.Fatalf("expected custom balancer addr 127.0.0.1:29999, got %s", resolved)
	}
}

func TestResolveDestinationsForAttemptsRoundRobinCandidates(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "round-robin"},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "payment-v1",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28081", Weight: 100},
						{Addr: "127.0.0.1:28082", Weight: 100},
						{Addr: "127.0.0.1:28083", Weight: 100},
					},
				},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	candidates, err := plan.ResolveDestinationsForAttempts("payment-v1", 3)
	if err != nil {
		t.Fatalf("resolve candidates failed: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}
	if candidates[0] != "127.0.0.1:28081" || candidates[1] != "127.0.0.1:28082" || candidates[2] != "127.0.0.1:28083" {
		t.Fatalf("unexpected candidate sequence: %+v", candidates)
	}
}

func TestResolveDestinationsForAttemptsLoadFirstFallbackOrder(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "payment-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "load-first"},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "payment-v1",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28081", Weight: 10},
						{Addr: "127.0.0.1:28082", Weight: 80},
						{Addr: "127.0.0.1:28083", Weight: 40},
					},
				},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	candidates, err := plan.ResolveDestinationsForAttempts("payment-v1", 3)
	if err != nil {
		t.Fatalf("resolve candidates failed: %v", err)
	}

	if candidates[0] != "127.0.0.1:28082" || candidates[1] != "127.0.0.1:28083" || candidates[2] != "127.0.0.1:28081" {
		t.Fatalf("unexpected load-first fallback order: %+v", candidates)
	}
}

func TestResolveDestinationsForAttemptsPrefersDirectBeforeRelay(t *testing.T) {
	services := []model.ServiceConfig{
		{
			Name: "relay-priority-svc",
			TrafficPolicy: model.ServiceTrafficPolicy{
				Listener: model.ListenerPolicy{Addr: "0.0.0.0", Port: 18080},
				LB:       model.LBPolicy{Strategy: "load-first"},
			},
			BackendGroups: []model.BackendGroup{
				{
					Name: "relay-priority-group",
					Targets: []model.BackendTarget{
						{Addr: "127.0.0.1:28100", Weight: 20},
						{Addr: "127.0.0.1:28101", Weight: 90, Tags: map[string]string{"relay": "true"}},
						{Addr: "127.0.0.1:28102", Weight: 10, Tags: map[string]string{"relay": "true"}},
					},
				},
			},
			Routes: []model.ServiceRoute{{PathPrefix: "/", Destination: "relay-priority-group", Weight: 100}},
		},
	}

	plan, err := BuildPlan(services)
	if err != nil {
		t.Fatalf("build plan failed: %v", err)
	}

	candidates, err := plan.ResolveDestinationsForAttempts("relay-priority-group", 3)
	if err != nil {
		t.Fatalf("resolve candidates failed: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(candidates))
	}

	if candidates[0] != "127.0.0.1:28100" || candidates[1] != "127.0.0.1:28101" || candidates[2] != "127.0.0.1:28102" {
		t.Fatalf("expected direct-first relay-fallback order, got %+v", candidates)
	}
}
