package traffic

import (
	"sort"
	"testing"

	"fluxmesh/internal/model"
)

func TestMatchWithoutMatcherFallback(t *testing.T) {
	listener := ListenerKey{Addr: "0.0.0.0", Port: 18080}
	key := listenerMapKey(listener.Addr, listener.Port)
	plan := Plan{
		listeners: map[string]ListenerPlan{
			key: {
				Listener: listener,
				Routes: []RouteBinding{
					{ServiceName: "svc-wild", Hosts: []string{"*"}, PathPrefix: "/", Destination: "dst-wild", Weight: 10},
					{ServiceName: "svc-exact", Hosts: []string{"api.example.com"}, PathPrefix: "/api", Destination: "dst-api", Weight: 80},
				},
			},
		},
	}

	got, ok := plan.Match("0.0.0.0", 18080, "api.example.com", "/api/v1/orders")
	if !ok {
		t.Fatalf("expected match")
	}
	if got.ServiceName != "svc-exact" || got.Destination != "dst-api" {
		t.Fatalf("unexpected match result: %+v", got)
	}

	got, ok = plan.Match("0.0.0.0", 18080, "unknown.example.com", "/x")
	if !ok {
		t.Fatalf("expected wildcard match")
	}
	if got.ServiceName != "svc-wild" {
		t.Fatalf("expected wildcard route, got %+v", got)
	}

	if _, ok := plan.Match("0.0.0.0", 18080, "api.example.com", "/not-api"); !ok {
		t.Fatalf("expected fallback wildcard when exact path misses")
	}
}

func TestBestRouteMatch(t *testing.T) {
	listener := ListenerKey{Addr: "127.0.0.1", Port: 8080}
	routes := []RouteBinding{
		{ServiceName: "svc-a", PathPrefix: "/a", Destination: "d-a", Weight: 10},
		{ServiceName: "svc-b", PathPrefix: "/a/deep", Destination: "d-b", Weight: 5},
	}

	result, ok := bestRouteMatch(listener, routes, "/a/deep/x", 2)
	if !ok {
		t.Fatalf("expected best route match")
	}
	if result.ServiceName != "svc-b" {
		t.Fatalf("expected deeper prefix route, got %+v", result)
	}

	if _, ok := bestRouteMatch(listener, routes, "/none", 2); ok {
		t.Fatalf("expected no route match")
	}
}

func TestShuffleBackendTargets(t *testing.T) {
	items := []model.BackendTarget{{Addr: "a", Weight: 1}, {Addr: "b", Weight: 1}, {Addr: "c", Weight: 1}}

	nilStateItems := append([]model.BackendTarget(nil), items...)
	shuffleBackendTargets(nilStateItems, nil)
	if len(nilStateItems) != len(items) {
		t.Fatalf("unexpected length after shuffle")
	}

	stateItems := append([]model.BackendTarget(nil), items...)
	shuffleBackendTargets(stateItems, newPlanState())
	if len(stateItems) != len(items) {
		t.Fatalf("unexpected length after state shuffle")
	}

	gotSet := make([]string, 0, len(stateItems))
	for _, item := range stateItems {
		gotSet = append(gotSet, item.Addr)
	}
	sort.Strings(gotSet)
	if gotSet[0] != "a" || gotSet[1] != "b" || gotSet[2] != "c" {
		t.Fatalf("expected shuffled items to preserve membership, got %v", gotSet)
	}
}

func TestIsDirectDestination(t *testing.T) {
	if !isDirectDestination("127.0.0.1:8080") {
		t.Fatalf("expected host:port as direct destination")
	}
	if !isDirectDestination("https://example.com:443") {
		t.Fatalf("expected https URL as direct destination")
	}
	if isDirectDestination("grpc://example.com:443") {
		t.Fatalf("expected unsupported scheme to be rejected")
	}
	if isDirectDestination("not-a-destination") {
		t.Fatalf("expected invalid destination to be rejected")
	}
}

func TestPlanSnapshotOrderingMethods(t *testing.T) {
	plan := Plan{
		udpBindings: map[string]UDPBinding{
			listenerMapKey("127.0.0.1", 3002): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 3002}},
			listenerMapKey("127.0.0.1", 3001): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 3001}},
		},
		tcpBindings: map[string]TCPBinding{
			listenerMapKey("127.0.0.1", 4002): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 4002}},
			listenerMapKey("127.0.0.1", 4001): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 4001}},
		},
		listeners: map[string]ListenerPlan{
			listenerMapKey("127.0.0.1", 5002): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 5002}},
			listenerMapKey("127.0.0.1", 5001): {Listener: ListenerKey{Addr: "127.0.0.1", Port: 5001}},
		},
	}

	udp := plan.UDPBindings()
	if len(udp) != 2 || udp[0].Listener.Port != 3001 || udp[1].Listener.Port != 3002 {
		t.Fatalf("unexpected udp binding order: %+v", udp)
	}
	if _, ok := plan.UDPBinding("127.0.0.1", 3001); !ok {
		t.Fatalf("expected udp binding lookup success")
	}

	tcp := plan.TCPBindings()
	if len(tcp) != 2 || tcp[0].Listener.Port != 4001 || tcp[1].Listener.Port != 4002 {
		t.Fatalf("unexpected tcp binding order: %+v", tcp)
	}
	if _, ok := plan.TCPBinding("127.0.0.1", 4999); ok {
		t.Fatalf("expected tcp binding lookup miss")
	}

	listeners := plan.Listeners()
	if len(listeners) != 2 || listeners[0].Listener.Port != 5001 || listeners[1].Listener.Port != 5002 {
		t.Fatalf("unexpected listener order: %+v", listeners)
	}
}
