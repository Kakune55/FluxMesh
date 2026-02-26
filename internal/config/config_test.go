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
