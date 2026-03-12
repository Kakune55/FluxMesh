package model

import "testing"

func TestServiceConfigValidate(t *testing.T) {
	valid := ServiceConfig{
		Name: "payment-svc",
		Routes: []ServiceRoute{
			{PathPrefix: "/", Destination: "payment-v1", Weight: 100},
		},
	}

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
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc"}},
			},
		},
		{
			name: "empty routes",
			cfg: ServiceConfig{
				Name: "payment-svc",
			},
		},
		{
			name: "route without destination",
			cfg: ServiceConfig{
				Name:   "payment-svc",
				Routes: []ServiceRoute{{PathPrefix: "/"}},
			},
		},
		{
			name: "invalid weight",
			cfg: ServiceConfig{
				Name:   "payment-svc",
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: 101}},
			},
		},
		{
			name: "negative weight",
			cfg: ServiceConfig{
				Name:   "payment-svc",
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc", Weight: -1}},
			},
		},
		{
			name: "blank name",
			cfg: ServiceConfig{
				Name:   "   ",
				Routes: []ServiceRoute{{PathPrefix: "/", Destination: "svc"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}
