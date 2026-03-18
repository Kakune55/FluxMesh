package model

import "testing"

func TestServiceConfigValidate(t *testing.T) {
	valid := ServiceConfig{
		Name: "payment-svc",
		TrafficPolicy: ServiceTrafficPolicy{
			Listener: ListenerPolicy{Port: 18080},
		},
		Routes: []ServiceRoute{
			{PathPrefix: "/", Destination: "payment-v1", Weight: 100},
		},
	}
	valid.ApplyDefaults()

	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestServiceConfigValidateInvalid(t *testing.T) {
	tests := []struct {
		name string
		cfg  ServiceConfig
	}{
		{
			name: "missing name",
			cfg: ServiceConfig{
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Port: 18080}},
				Routes:        []ServiceRoute{{PathPrefix: "/", Destination: "svc"}},
			},
		},
		{
			name: "empty routes",
			cfg: ServiceConfig{
				Name:          "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Port: 18080}},
			},
		},
		{
			name: "route without destination",
			cfg: ServiceConfig{
				Name:          "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Port: 18080}},
				Routes:        []ServiceRoute{{PathPrefix: "/"}},
			},
		},
		{
			name: "invalid weight",
			cfg: ServiceConfig{
				Name:          "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Port: 18080}},
				Routes:        []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: 101}},
			},
		},
		{
			name: "negative weight",
			cfg: ServiceConfig{
				Name:          "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Port: 18080}},
				Routes:        []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: -1}},
			},
		},
		{
			name: "blank name",
			cfg: ServiceConfig{
				Name:          "   ",
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Port: 18080}},
				Routes:        []ServiceRoute{{PathPrefix: "/", Destination: "svc"}},
			},
		},
		{
			name: "missing listener port",
			cfg: ServiceConfig{
				Name:          "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{Listener: ListenerPolicy{Addr: "0.0.0.0"}},
				Routes:        []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: 100}},
			},
		},
		{
			name: "invalid listener addr",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener: ListenerPolicy{Addr: "example.com", Port: 18080},
				},
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: 100}},
			},
		},
		{
			name: "invalid protocol",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener:  ListenerPolicy{Port: 18080},
					Protocols: []string{"grpc"},
				},
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: 100}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.ApplyDefaults()
			if err := tt.cfg.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestServiceConfigApplyDefaults(t *testing.T) {
	cfg := ServiceConfig{
		Name: "payment-svc",
		TrafficPolicy: ServiceTrafficPolicy{
			Listener: ListenerPolicy{Port: 18080},
		},
		BackendGroups: []BackendGroup{
			{
				Name:    "payment-v1",
				Targets: []BackendTarget{{Addr: "127.0.0.1:28081"}},
			},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "payment-v1"}},
	}

	cfg.ApplyDefaults()

	if cfg.TrafficPolicy.Listener.Addr != "0.0.0.0" {
		t.Fatalf("expected listener addr default 0.0.0.0, got %s", cfg.TrafficPolicy.Listener.Addr)
	}
	if cfg.TrafficPolicy.Proxy.Layer != "l7-http" {
		t.Fatalf("expected proxy layer default l7-http, got %s", cfg.TrafficPolicy.Proxy.Layer)
	}
	if len(cfg.TrafficPolicy.Protocols) != 1 || cfg.TrafficPolicy.Protocols[0] != "http" {
		t.Fatalf("expected protocol default [http], got %+v", cfg.TrafficPolicy.Protocols)
	}
	if len(cfg.Routes[0].Hosts) != 1 || cfg.Routes[0].Hosts[0] != "*" {
		t.Fatalf("expected route hosts default [*], got %+v", cfg.Routes[0].Hosts)
	}
	if cfg.Routes[0].Weight != 100 {
		t.Fatalf("expected route weight default 100, got %d", cfg.Routes[0].Weight)
	}
	if cfg.BackendGroups[0].Targets[0].Weight != 100 {
		t.Fatalf("expected backend target weight default 100, got %d", cfg.BackendGroups[0].Targets[0].Weight)
	}
}
