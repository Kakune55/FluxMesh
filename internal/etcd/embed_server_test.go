package etcd

import (
	"strings"
	"testing"

	"fluxmesh/internal/config"
)

func TestParseURL(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		u, err := parseURL("http://127.0.0.1:2379")
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}
		if u.Scheme != "http" || u.Host != "127.0.0.1:2379" {
			t.Fatalf("unexpected url: %s://%s", u.Scheme, u.Host)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := parseURL("://bad-url")
		if err == nil {
			t.Fatalf("expected parse error")
		}
	})
}

func TestStartEmbeddedServerInvalidURLs(t *testing.T) {
	base := config.Config{
		NodeID:             "node-a",
		DataDir:            t.TempDir(),
		ClusterState:       config.ClusterStateNew,
		ClientListenURL:    "http://127.0.0.1:2379",
		ClientAdvertiseURL: "http://127.0.0.1:2379",
		PeerListenURL:      "http://127.0.0.1:2380",
		PeerAdvertiseURL:   "http://127.0.0.1:2380",
	}

	tests := []struct {
		name     string
		mutate   func(*config.Config)
		contains string
	}{
		{
			name: "invalid client listen",
			mutate: func(c *config.Config) {
				c.ClientListenURL = "://bad"
			},
			contains: "invalid client-listen-url",
		},
		{
			name: "invalid client advertise",
			mutate: func(c *config.Config) {
				c.ClientAdvertiseURL = "://bad"
			},
			contains: "invalid client-advertise-url",
		},
		{
			name: "invalid peer listen",
			mutate: func(c *config.Config) {
				c.PeerListenURL = "://bad"
			},
			contains: "invalid peer-listen-url",
		},
		{
			name: "invalid peer advertise",
			mutate: func(c *config.Config) {
				c.PeerAdvertiseURL = "://bad"
			},
			contains: "invalid peer-advertise-url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)

			_, err := StartEmbeddedServer(cfg, "node-a=http://127.0.0.1:2380")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected error containing %q, got %v", tc.contains, err)
			}
		})
	}
}

func TestEmbeddedServerCloseNilSafe(t *testing.T) {
	var s *EmbeddedServer
	s.Close()

	s = &EmbeddedServer{}
	s.Close()
}
