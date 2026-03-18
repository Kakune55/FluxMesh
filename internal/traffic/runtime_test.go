package traffic

import (
	"testing"

	"fluxmesh/internal/model"
)

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
