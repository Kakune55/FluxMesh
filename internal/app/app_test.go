package app

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"fluxmesh/internal/config"
	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"
	"fluxmesh/internal/softkv"
	"fluxmesh/internal/sysmetrics"
	"fluxmesh/internal/testutil/etcdtest"
)

type fakeMetricsCollector struct {
	snapshot sysmetrics.Snapshot
	err      error
}

func (f fakeMetricsCollector) Collect() (sysmetrics.Snapshot, error) {
	if f.err != nil {
		return sysmetrics.Snapshot{}, f.err
	}
	return f.snapshot, nil
}

type fakeSoftStateWriter struct {
	err       error
	result    softkv.WriteResult
	callCount int
	lastKey   string
	lastValue any
	lastTTL   time.Duration
	lastSrc   string
}

func (f *fakeSoftStateWriter) Write(_ context.Context, key string, value any, ttl time.Duration, sourceID string) (softkv.WriteResult, error) {
	f.callCount++
	f.lastKey = key
	f.lastValue = value
	f.lastTTL = ttl
	f.lastSrc = sourceID
	if f.err != nil {
		return softkv.WriteResult{}, f.err
	}
	return f.result, nil
}

func TestRound2(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{name: "round down", input: 12.344, want: 12.34},
		{name: "round up", input: 12.345, want: 12.35},
		{name: "negative", input: -1.235, want: -1.24},
		{name: "zero", input: 0, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := round2(tc.input)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestNewClientValidation(t *testing.T) {
	cli, err := newClient(nil)
	if err == nil {
		t.Fatalf("expected empty endpoint error")
	}
	if cli != nil {
		t.Fatalf("expected nil client on invalid input")
	}
}

func TestNewClientSuccess(t *testing.T) {
	emb := etcdtest.Start(t)
	cli, err := newClient([]string{emb.Client.Endpoints()[0]})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if cli == nil {
		t.Fatalf("expected non-nil client")
	}
	t.Cleanup(func() {
		_ = cli.Close()
	})
}

func TestDiscoverGossipPeers(t *testing.T) {
	t.Run("nil nodes service", func(t *testing.T) {
		a := &App{cfg: config.Config{NodeID: "self"}}
		peers := a.discoverGossipPeers(context.Background())
		if peers != nil {
			t.Fatalf("expected nil peers when nodes service is nil, got %v", peers)
		}
	})

	t.Run("filters self, empty ip and deduplicates", func(t *testing.T) {
		emb := etcdtest.Start(t)
		nodesSvc := registry.NewService(emb.Client)

		keepAliveCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		seed := []model.Node{
			{
				ID:      "a-self",
				IP:      "10.10.0.1",
				Version: "v1",
				NodeStatus: model.NodeStatus{
					NodeRole:   "agent",
					NodeStatus: "Ready",
				},
			},
			{
				ID:      "b-peer-1",
				IP:      "10.10.0.2",
				Version: "v1",
				NodeStatus: model.NodeStatus{
					NodeRole:   "agent",
					NodeStatus: "Ready",
				},
			},
			{
				ID:      "c-peer-dup",
				IP:      "10.10.0.2",
				Version: "v1",
				NodeStatus: model.NodeStatus{
					NodeRole:   "agent",
					NodeStatus: "Ready",
				},
			},
			{
				ID:      "d-empty-ip",
				IP:      "",
				Version: "v1",
				NodeStatus: model.NodeStatus{
					NodeRole:   "agent",
					NodeStatus: "Ready",
				},
			},
		}

		for i := range seed {
			if _, _, err := nodesSvc.RegisterWithLease(t.Context(), keepAliveCtx, seed[i], 30); err != nil {
				t.Fatalf("register node %s failed: %v", seed[i].ID, err)
			}
		}

		a := &App{
			cfg:   config.Config{NodeID: "a-self"},
			nodes: nodesSvc,
		}

		peers := a.discoverGossipPeers(t.Context())
		sort.Strings(peers)
		want := []string{net.JoinHostPort("10.10.0.2", strconv.Itoa(softkv.DefaultGossipPort))}
		if !reflect.DeepEqual(peers, want) {
			t.Fatalf("unexpected peers, want %v got %v", want, peers)
		}
	})

	t.Run("list error fallback", func(t *testing.T) {
		emb := etcdtest.Start(t)
		nodesSvc := registry.NewService(emb.Client)
		a := &App{cfg: config.Config{NodeID: "self"}, nodes: nodesSvc}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		peers := a.discoverGossipPeers(ctx)
		if peers != nil {
			t.Fatalf("expected nil peers on list error, got %v", peers)
		}
	})
}

func TestUpdateNodeMetrics(t *testing.T) {
	node := model.Node{ID: "node-a"}

	t.Run("collector error", func(t *testing.T) {
		writer := &fakeSoftStateWriter{}
		a := &App{
			metrics:    fakeMetricsCollector{err: errors.New("collect failed")},
			softStore:  softkv.NewStore(),
			softWriter: writer,
			selfNode:   node,
		}

		a.updateNodeMetrics(context.Background())
		if writer.callCount != 0 {
			t.Fatalf("expected no writes on collect error, got %d", writer.callCount)
		}
	})

	t.Run("no soft store skips write", func(t *testing.T) {
		writer := &fakeSoftStateWriter{}
		a := &App{
			metrics: fakeMetricsCollector{snapshot: sysmetrics.Snapshot{
				CPUUsage:      1,
				MemoryUsage:   2,
				SystemLoad1m:  3,
				SystemLoad5m:  4,
				SystemLoad15m: 5,
			}},
			softStore:  nil,
			softWriter: writer,
			selfNode:   node,
		}

		a.updateNodeMetrics(context.Background())
		if writer.callCount != 0 {
			t.Fatalf("expected no writes when soft store is nil, got %d", writer.callCount)
		}
	})

	t.Run("writer error does not panic", func(t *testing.T) {
		writer := &fakeSoftStateWriter{err: errors.New("write failed")}
		a := &App{
			metrics: fakeMetricsCollector{snapshot: sysmetrics.Snapshot{
				CPUUsage:      10.111,
				MemoryUsage:   20.222,
				SystemLoad1m:  0.333,
				SystemLoad5m:  0.444,
				SystemLoad15m: 0.555,
			}},
			softStore:  softkv.NewStore(),
			softWriter: writer,
			selfNode:   node,
		}

		a.updateNodeMetrics(context.Background())
		if writer.callCount != 1 {
			t.Fatalf("expected one write attempt, got %d", writer.callCount)
		}
		if writer.lastKey != "metrics/nodes/node-a" {
			t.Fatalf("unexpected key: %s", writer.lastKey)
		}
		if writer.lastTTL != 30*time.Second {
			t.Fatalf("unexpected ttl: %v", writer.lastTTL)
		}
		if writer.lastSrc != "node-a" {
			t.Fatalf("unexpected source id: %s", writer.lastSrc)
		}
	})

	t.Run("publish error path still writes rounded snapshot", func(t *testing.T) {
		writer := &fakeSoftStateWriter{result: softkv.WriteResult{PublishErr: errors.New("publish failed")}}
		a := &App{
			metrics: fakeMetricsCollector{snapshot: sysmetrics.Snapshot{
				CPUUsage:      12.345,
				MemoryUsage:   67.891,
				SystemLoad1m:  1.234,
				SystemLoad5m:  2.345,
				SystemLoad15m: 3.456,
			}},
			softStore:  softkv.NewStore(),
			softWriter: writer,
			selfNode:   node,
		}

		a.updateNodeMetrics(context.Background())
		if writer.callCount != 1 {
			t.Fatalf("expected one write, got %d", writer.callCount)
		}

		snap, ok := writer.lastValue.(sysmetrics.Snapshot)
		if !ok {
			t.Fatalf("expected snapshot value type, got %T", writer.lastValue)
		}
		if snap.CPUUsage != 12.35 || snap.MemoryUsage != 67.89 || snap.SystemLoad1m != 1.23 || snap.SystemLoad5m != 2.35 || snap.SystemLoad15m != 3.46 {
			t.Fatalf("unexpected rounded snapshot: %+v", snap)
		}
	})
}

func TestRecoverLease(t *testing.T) {
	t.Run("context canceled before start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		a := &App{}
		err := a.recoverLease(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("register success first try", func(t *testing.T) {
		calls := 0
		a := &App{
			leaseRegFn: func(context.Context) error {
				calls++
				return nil
			},
			backoffWait: func(context.Context, time.Duration) error {
				t.Fatalf("backoff should not be called on first success")
				return nil
			},
		}

		if err := a.recoverLease(context.Background()); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected one register call, got %d", calls)
		}
	})

	t.Run("retry then success", func(t *testing.T) {
		calls := 0
		waitCalls := 0
		waitDurations := make([]time.Duration, 0, 2)

		a := &App{
			leaseRegFn: func(context.Context) error {
				calls++
				if calls < 3 {
					return errors.New("temporary error")
				}
				return nil
			},
			backoffWait: func(_ context.Context, d time.Duration) error {
				waitCalls++
				waitDurations = append(waitDurations, d)
				return nil
			},
		}

		if err := a.recoverLease(context.Background()); err != nil {
			t.Fatalf("expected success after retry, got %v", err)
		}
		if calls != 3 {
			t.Fatalf("expected 3 register attempts, got %d", calls)
		}
		if waitCalls != 2 {
			t.Fatalf("expected 2 backoff waits, got %d", waitCalls)
		}
		if !reflect.DeepEqual(waitDurations, []time.Duration{time.Second, 2 * time.Second}) {
			t.Fatalf("unexpected backoff sequence: %v", waitDurations)
		}
	})

	t.Run("wait interrupted by canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		a := &App{
			leaseRegFn: func(context.Context) error {
				cancel()
				return errors.New("still failing")
			},
			backoffWait: func(ctx context.Context, _ time.Duration) error {
				<-ctx.Done()
				return ctx.Err()
			},
		}

		err := a.recoverLease(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})
}

func TestWaitBackoffWithContext(t *testing.T) {
	t.Run("wait completes", func(t *testing.T) {
		err := waitBackoffWithContext(context.Background(), time.Millisecond)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := waitBackoffWithContext(ctx, time.Second)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})
}

func TestRunSoftKVBus(t *testing.T) {
	t.Run("nil store or bus returns immediately", func(t *testing.T) {
		memberlistCalls := 0
		loopbackCalls := 0
		oldMemberlist := runMemberlistFn
		oldLoopback := runLoopbackFn
		runMemberlistFn = func(context.Context, *softkv.Store, <-chan softkv.Event, softkv.MemberlistOptions) error {
			memberlistCalls++
			return nil
		}
		runLoopbackFn = func(context.Context, *softkv.Store, <-chan softkv.Event) {
			loopbackCalls++
		}
		t.Cleanup(func() {
			runMemberlistFn = oldMemberlist
			runLoopbackFn = oldLoopback
		})

		(&App{}).runSoftKVBus(context.Background())
		(&App{softStore: softkv.NewStore()}).runSoftKVBus(context.Background())
		(&App{softBus: softkv.NewBus(1)}).runSoftKVBus(context.Background())

		if memberlistCalls != 0 || loopbackCalls != 0 {
			t.Fatalf("expected no softkv runtime calls, got memberlist=%d loopback=%d", memberlistCalls, loopbackCalls)
		}
	})

	t.Run("memberlist success", func(t *testing.T) {
		memberlistCalls := 0
		loopbackCalls := 0
		captured := softkv.MemberlistOptions{}

		oldMemberlist := runMemberlistFn
		oldLoopback := runLoopbackFn
		runMemberlistFn = func(_ context.Context, _ *softkv.Store, _ <-chan softkv.Event, opts softkv.MemberlistOptions) error {
			memberlistCalls++
			captured = opts
			return nil
		}
		runLoopbackFn = func(context.Context, *softkv.Store, <-chan softkv.Event) {
			loopbackCalls++
		}
		t.Cleanup(func() {
			runMemberlistFn = oldMemberlist
			runLoopbackFn = oldLoopback
		})

		a := &App{
			cfg:       config.Config{NodeID: "node-x", IP: "10.0.0.9"},
			softStore: softkv.NewStore(),
			softBus:   softkv.NewBus(4),
		}
		a.runSoftKVBus(context.Background())

		if memberlistCalls != 1 {
			t.Fatalf("expected one memberlist call, got %d", memberlistCalls)
		}
		if loopbackCalls != 0 {
			t.Fatalf("expected no loopback fallback, got %d", loopbackCalls)
		}
		if captured.NodeID != "node-x" || captured.AdvertiseAddr != "10.0.0.9" || captured.BindPort != softkv.DefaultGossipPort {
			t.Fatalf("unexpected memberlist options: %+v", captured)
		}
	})

	t.Run("memberlist error falls back to loopback", func(t *testing.T) {
		memberlistCalls := 0
		loopbackCalls := 0

		oldMemberlist := runMemberlistFn
		oldLoopback := runLoopbackFn
		runMemberlistFn = func(context.Context, *softkv.Store, <-chan softkv.Event, softkv.MemberlistOptions) error {
			memberlistCalls++
			return errors.New("memberlist failed")
		}
		runLoopbackFn = func(context.Context, *softkv.Store, <-chan softkv.Event) {
			loopbackCalls++
		}
		t.Cleanup(func() {
			runMemberlistFn = oldMemberlist
			runLoopbackFn = oldLoopback
		})

		a := &App{
			cfg:       config.Config{NodeID: "node-y", IP: "10.0.0.8"},
			softStore: softkv.NewStore(),
			softBus:   softkv.NewBus(4),
		}
		a.runSoftKVBus(context.Background())

		if memberlistCalls != 1 || loopbackCalls != 1 {
			t.Fatalf("expected memberlist=1 and loopback=1, got memberlist=%d loopback=%d", memberlistCalls, loopbackCalls)
		}
	})
}

func TestGCSoftState(t *testing.T) {
	t.Run("nil store returns immediately", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			(&App{}).gcSoftState(context.Background())
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("expected immediate return when store is nil")
		}
	})

	t.Run("canceled context returns quickly", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		done := make(chan struct{})
		go func() {
			(&App{softStore: softkv.NewStore()}).gcSoftState(ctx)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("expected quick return when context is canceled")
		}
	})
}
