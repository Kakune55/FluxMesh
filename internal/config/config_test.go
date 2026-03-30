package config

import (
	"flag"
	"io"
	"os"
	"testing"
)

func withIsolatedFlags(t *testing.T, args []string, fn func()) {
	t.Helper()

	originalCommandLine := flag.CommandLine
	originalArgs := os.Args

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flag.CommandLine = fs
	os.Args = args

	t.Cleanup(func() {
		flag.CommandLine = originalCommandLine
		os.Args = originalArgs
	})

	fn()
}

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
			cfg:  Config{Role: Role("bad"), NodeID: "n1", LeaseTTLSeconds: 10},
		},
		{
			name: "invalid cluster state for server",
			cfg:  Config{Role: RoleServer, ClusterState: ClusterState("bad"), NodeID: "n1", LeaseTTLSeconds: 10},
		},
		{
			name: "missing node id",
			cfg:  Config{Role: RoleServer, ClusterState: ClusterStateNew, LeaseTTLSeconds: 10},
		},
		{
			name: "invalid lease ttl",
			cfg:  Config{Role: RoleServer, ClusterState: ClusterStateNew, NodeID: "n1", LeaseTTLSeconds: 0},
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

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("FLUXMESH_ROLE", "agent")
	t.Setenv("FLUXMESH_CLUSTER_STATE", "new")
	t.Setenv("FLUXMESH_NODE_ID", "agent-1")
	t.Setenv("FLUXMESH_IP", "10.0.0.2")
	t.Setenv("FLUXMESH_VERSION", "v9.9.9")
	t.Setenv("FLUXMESH_DATA_DIR", "/tmp/fm-data")
	t.Setenv("FLUXMESH_ADMIN_ADDR", ":18080")
	t.Setenv("FLUXMESH_CLIENT_LISTEN_URL", "http://0.0.0.0:32379")
	t.Setenv("FLUXMESH_CLIENT_ADVERTISE_URL", "http://10.0.0.2:32379")
	t.Setenv("FLUXMESH_PEER_LISTEN_URL", "http://0.0.0.0:32380")
	t.Setenv("FLUXMESH_PEER_ADVERTISE_URL", "http://10.0.0.2:32380")
	t.Setenv("FLUXMESH_SEED_ENDPOINTS", "http://10.0.0.1:2379, http://10.0.0.3:2379")
	t.Setenv("FLUXMESH_LEASE_TTL", "25")

	withIsolatedFlags(t, []string{"fluxmesh"}, func() {
		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.Role != RoleAgent {
			t.Fatalf("expected role %q, got %q", RoleAgent, cfg.Role)
		}
		if cfg.NodeID != "agent-1" {
			t.Fatalf("expected node-id agent-1, got %q", cfg.NodeID)
		}
		if cfg.LeaseTTLSeconds != 25 {
			t.Fatalf("expected lease-ttl 25, got %d", cfg.LeaseTTLSeconds)
		}
		if len(cfg.SeedEndpoints) != 2 {
			t.Fatalf("expected 2 seed endpoints, got %d", len(cfg.SeedEndpoints))
		}
	})
}

func TestLoadFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("FLUXMESH_ROLE", "agent")
	t.Setenv("FLUXMESH_NODE_ID", "env-node")
	t.Setenv("FLUXMESH_LEASE_TTL", "10")
	t.Setenv("FLUXMESH_SEED_ENDPOINTS", "http://10.0.0.1:2379")

	args := []string{
		"fluxmesh",
		"-role=server",
		"-cluster-state=new",
		"-node-id=flag-node",
		"-lease-ttl=30",
	}

	withIsolatedFlags(t, args, func() {
		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.Role != RoleServer {
			t.Fatalf("expected role %q, got %q", RoleServer, cfg.Role)
		}
		if cfg.NodeID != "flag-node" {
			t.Fatalf("expected node-id flag-node, got %q", cfg.NodeID)
		}
		if cfg.LeaseTTLSeconds != 30 {
			t.Fatalf("expected lease-ttl 30, got %d", cfg.LeaseTTLSeconds)
		}
	})
}

func TestLoadValidationError(t *testing.T) {
	t.Setenv("FLUXMESH_ROLE", "agent")
	t.Setenv("FLUXMESH_NODE_ID", "")
	t.Setenv("FLUXMESH_SEED_ENDPOINTS", "http://10.0.0.1:2379")

	withIsolatedFlags(t, []string{"fluxmesh"}, func() {
		_, err := Load()
		if err == nil {
			t.Fatalf("expected validation error")
		}
	})
}
