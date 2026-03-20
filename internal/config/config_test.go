package config

import "testing"

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid server new",
			cfg: Config{
				Role:            RoleServer,
				ClusterState:    ClusterStateNew,
				NodeID:          "s1",
				LeaseTTLSeconds: 10,
			},
			wantErr: false,
		},
		{
			name: "agent requires seed",
			cfg: Config{
				Role:            RoleAgent,
				NodeID:          "a1",
				LeaseTTLSeconds: 10,
			},
			wantErr: true,
		},
		{
			name: "existing server requires seed",
			cfg: Config{
				Role:            RoleServer,
				ClusterState:    ClusterStateExisting,
				NodeID:          "s2",
				LeaseTTLSeconds: 10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfigValidateInvalidBasics(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "invalid role",
			cfg: Config{Role: Role("bad"), NodeID: "n1", LeaseTTLSeconds: 10},
		},
		{
			name: "invalid cluster state for server",
			cfg: Config{Role: RoleServer, ClusterState: ClusterState("bad"), NodeID: "n1", LeaseTTLSeconds: 10},
		},
		{
			name: "missing node id",
			cfg: Config{Role: RoleServer, ClusterState: ClusterStateNew, LeaseTTLSeconds: 10},
		},
		{
			name: "invalid lease ttl",
			cfg: Config{Role: RoleServer, ClusterState: ClusterStateNew, NodeID: "n1", LeaseTTLSeconds: 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a, ,b,, c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("expected %d items, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %q at %d, got %q", want[i], i, got[i])
		}
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Run("envOr fallback", func(t *testing.T) {
		t.Setenv("FM_TEST_ENV_OR", "")
		if got := envOr("FM_TEST_ENV_OR", "fallback"); got != "fallback" {
			t.Fatalf("expected fallback, got %q", got)
		}
	})

	t.Run("envOr value", func(t *testing.T) {
		t.Setenv("FM_TEST_ENV_OR", " value ")
		if got := envOr("FM_TEST_ENV_OR", "fallback"); got != "value" {
			t.Fatalf("expected trimmed value, got %q", got)
		}
	})

	t.Run("envOrInt64 fallback empty", func(t *testing.T) {
		t.Setenv("FM_TEST_ENV_INT", "")
		if got := envOrInt64("FM_TEST_ENV_INT", 42); got != 42 {
			t.Fatalf("expected fallback 42, got %d", got)
		}
	})

	t.Run("envOrInt64 fallback invalid", func(t *testing.T) {
		t.Setenv("FM_TEST_ENV_INT", "bad")
		if got := envOrInt64("FM_TEST_ENV_INT", 42); got != 42 {
			t.Fatalf("expected fallback 42, got %d", got)
		}
	})

	t.Run("envOrInt64 value", func(t *testing.T) {
		t.Setenv("FM_TEST_ENV_INT", "123")
		if got := envOrInt64("FM_TEST_ENV_INT", 42); got != 123 {
			t.Fatalf("expected 123, got %d", got)
		}
	})
}
