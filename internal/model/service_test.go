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
		{
			name: "invalid lb strategy",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener: ListenerPolicy{Port: 18080},
					LB:       LBPolicy{Strategy: "hash@v1"},
				},
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: 100}},
			},
		},
		{
			name: "duplicate backend group name",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener: ListenerPolicy{Port: 18080},
				},
				BackendGroups: []BackendGroup{
					{Name: "bg", Targets: []BackendTarget{{Addr: "127.0.0.1:28081", Weight: 100}}},
					{Name: "bg", Targets: []BackendTarget{{Addr: "127.0.0.1:28082", Weight: 100}}},
				},
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "bg", Weight: 100}},
			},
		},
		{
			name: "backend group without targets",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener: ListenerPolicy{Port: 18080},
				},
				BackendGroups: []BackendGroup{{Name: "bg", Targets: []BackendTarget{}}},
				Routes:        []ServiceRoute{{PathPrefix: "/", Destination: "bg", Weight: 100}},
			},
		},
		{
			name: "backend target invalid addr",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener: ListenerPolicy{Port: 18080},
				},
				BackendGroups: []BackendGroup{{
					Name:    "bg",
					Targets: []BackendTarget{{Addr: "127.0.0.1", Weight: 100}},
				}},
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "bg", Weight: 100}},
			},
		},
		{
			name: "backend target invalid weight",
			cfg: ServiceConfig{
				Name: "payment-svc",
				TrafficPolicy: ServiceTrafficPolicy{
					Listener: ListenerPolicy{Port: 18080},
				},
				BackendGroups: []BackendGroup{{
					Name:    "bg",
					Targets: []BackendTarget{{Addr: "127.0.0.1:28081", Weight: 101}},
				}},
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "bg", Weight: 100}},
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
	if cfg.TrafficPolicy.Observability.MetricsSampleRate != 1 {
		t.Fatalf("expected observability sample rate default 1, got %d", cfg.TrafficPolicy.Observability.MetricsSampleRate)
	}
}

func TestServiceConfigValidateObservabilitySampleRate(t *testing.T) {
	cfg := ServiceConfig{
		Name: "payment-svc",
		TrafficPolicy: ServiceTrafficPolicy{
			Listener: ListenerPolicy{Port: 18080},
			Observability: ObservabilityPolicy{
				MetricsSampleRate: 10001,
			},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "payment-v1", Weight: 100}},
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for too large metrics sample rate")
	}
}

func TestServiceConfigApplyDefaultsL4TCP(t *testing.T) {
	cfg := ServiceConfig{
		Name: "tcp-gateway",
		TrafficPolicy: ServiceTrafficPolicy{
			Proxy:    ProxyPolicy{Layer: "l4-tcp"},
			Listener: ListenerPolicy{Port: 19090},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:3306"}},
	}

	cfg.ApplyDefaults()

	if len(cfg.TrafficPolicy.Protocols) != 1 || cfg.TrafficPolicy.Protocols[0] != "tcp" {
		t.Fatalf("expected protocol default [tcp] for l4-tcp, got %+v", cfg.TrafficPolicy.Protocols)
	}
}

func TestServiceConfigValidateL4ProtocolMismatch(t *testing.T) {
	cfg := ServiceConfig{
		Name: "tcp-gateway",
		TrafficPolicy: ServiceTrafficPolicy{
			Proxy:     ProxyPolicy{Layer: "l4-tcp"},
			Protocols: []string{"http"},
			Listener:  ListenerPolicy{Port: 19090},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:3306", Weight: 100}},
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for l4-tcp protocol mismatch")
	}
}

func TestServiceConfigApplyDefaultsL4UDP(t *testing.T) {
	cfg := ServiceConfig{
		Name: "udp-gateway",
		TrafficPolicy: ServiceTrafficPolicy{
			Proxy:    ProxyPolicy{Layer: "l4-udp"},
			Listener: ListenerPolicy{Port: 19091},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:5353"}},
	}

	cfg.ApplyDefaults()

	if len(cfg.TrafficPolicy.Protocols) != 1 || cfg.TrafficPolicy.Protocols[0] != "udp" {
		t.Fatalf("expected protocol default [udp] for l4-udp, got %+v", cfg.TrafficPolicy.Protocols)
	}
}

func TestServiceConfigValidateL4UDPProtocolMismatch(t *testing.T) {
	cfg := ServiceConfig{
		Name: "udp-gateway",
		TrafficPolicy: ServiceTrafficPolicy{
			Proxy:     ProxyPolicy{Layer: "l4-udp"},
			Protocols: []string{"tcp"},
			Listener:  ListenerPolicy{Port: 19091},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:5353", Weight: 100}},
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for l4-udp protocol mismatch")
	}
}

func TestServiceConfigApplyDefaultsUDPPolicy(t *testing.T) {
	cfg := ServiceConfig{
		Name: "udp-gateway",
		TrafficPolicy: ServiceTrafficPolicy{
			Proxy:    ProxyPolicy{Layer: "l4-udp"},
			Listener: ListenerPolicy{Port: 19091},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:5353"}},
	}

	cfg.ApplyDefaults()

	if cfg.TrafficPolicy.UDP.DialTimeoutMs != 2000 {
		t.Fatalf("expected default udp dial timeout 2000ms, got %d", cfg.TrafficPolicy.UDP.DialTimeoutMs)
	}
	if cfg.TrafficPolicy.UDP.ReadTimeoutMs != 2000 {
		t.Fatalf("expected default udp read timeout 2000ms, got %d", cfg.TrafficPolicy.UDP.ReadTimeoutMs)
	}
	if cfg.TrafficPolicy.UDP.WriteTimeoutMs != 2000 {
		t.Fatalf("expected default udp write timeout 2000ms, got %d", cfg.TrafficPolicy.UDP.WriteTimeoutMs)
	}
	if cfg.TrafficPolicy.UDP.SessionTTLMs != 30000 {
		t.Fatalf("expected default udp session ttl 30000ms, got %d", cfg.TrafficPolicy.UDP.SessionTTLMs)
	}
	if cfg.TrafficPolicy.UDP.MaxPacketSize != 65535 {
		t.Fatalf("expected default udp max packet 65535, got %d", cfg.TrafficPolicy.UDP.MaxPacketSize)
	}
}

func TestServiceConfigValidateUDPPolicyRange(t *testing.T) {
	cfg := ServiceConfig{
		Name: "udp-gateway",
		TrafficPolicy: ServiceTrafficPolicy{
			Proxy:    ProxyPolicy{Layer: "l4-udp"},
			Listener: ListenerPolicy{Port: 19091},
			UDP:      UDPPolicy{DialTimeoutMs: 0, ReadTimeoutMs: 10, WriteTimeoutMs: 10, SessionTTLMs: 1000, MaxPacketSize: 1024},
			Protocols: []string{"udp"},
		},
		Routes: []ServiceRoute{{PathPrefix: "/", Destination: "127.0.0.1:5353", Weight: 100}},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error for invalid udp policy range")
	}
}
