package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type Role string

const (
	RoleServer Role = "server"
	RoleAgent  Role = "agent"
)

type ClusterState string

const (
	ClusterStateNew      ClusterState = "new"
	ClusterStateExisting ClusterState = "existing"
)

type Config struct {
	Role               Role
	ClusterState       ClusterState
	NodeID             string
	IP                 string
	Version            string
	DataDir            string
	AdminAddr          string
	ClientListenURL    string
	ClientAdvertiseURL string
	PeerListenURL      string
	PeerAdvertiseURL   string
	SeedEndpoints      []string
	LeaseTTLSeconds    int64
}

func Load() (Config, error) {
	var cfg Config

	flag.StringVar((*string)(&cfg.Role), "role", envOr("FLUXMESH_ROLE", "agent"), "node role: server|agent")
	flag.StringVar((*string)(&cfg.ClusterState), "cluster-state", envOr("FLUXMESH_CLUSTER_STATE", "new"), "cluster state for server: new|existing")
	flag.StringVar(&cfg.NodeID, "node-id", envOr("FLUXMESH_NODE_ID", ""), "node unique id")
	flag.StringVar(&cfg.IP, "ip", envOr("FLUXMESH_IP", "auto"), "node ip, use 'auto' to detect")
	flag.StringVar(&cfg.Version, "version", envOr("FLUXMESH_VERSION", "v0.1.0"), "node version")
	flag.StringVar(&cfg.DataDir, "data-dir", envOr("FLUXMESH_DATA_DIR", "./data"), "data dir for embedded etcd")
	flag.StringVar(&cfg.AdminAddr, "admin-addr", envOr("FLUXMESH_ADMIN_ADDR", ":15000"), "admin http listen addr")
	flag.StringVar(&cfg.ClientListenURL, "client-listen-url", envOr("FLUXMESH_CLIENT_LISTEN_URL", "http://0.0.0.0:2379"), "etcd client listen url")
	flag.StringVar(&cfg.ClientAdvertiseURL, "client-advertise-url", envOr("FLUXMESH_CLIENT_ADVERTISE_URL", ""), "etcd client advertise url")
	flag.StringVar(&cfg.PeerListenURL, "peer-listen-url", envOr("FLUXMESH_PEER_LISTEN_URL", "http://0.0.0.0:2380"), "etcd peer listen url")
	flag.StringVar(&cfg.PeerAdvertiseURL, "peer-advertise-url", envOr("FLUXMESH_PEER_ADVERTISE_URL", ""), "etcd peer advertise url")
	seedRaw := flag.String("seed-endpoints", envOr("FLUXMESH_SEED_ENDPOINTS", ""), "comma separated etcd endpoints")
	flag.Int64Var(&cfg.LeaseTTLSeconds, "lease-ttl", envOrInt64("FLUXMESH_LEASE_TTL", 10), "node lease ttl seconds")

	flag.Parse()

	cfg.SeedEndpoints = splitCSV(*seedRaw)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Role != RoleServer && c.Role != RoleAgent {
		return fmt.Errorf("invalid role: %s", c.Role)
	}
	if c.Role == RoleServer && c.ClusterState != ClusterStateNew && c.ClusterState != ClusterStateExisting {
		return fmt.Errorf("invalid cluster-state: %s", c.ClusterState)
	}
	if c.NodeID == "" {
		return errors.New("node-id is required")
	}
	if c.LeaseTTLSeconds <= 0 {
		return errors.New("lease-ttl must be > 0")
	}
	if c.Role == RoleAgent && len(c.SeedEndpoints) == 0 {
		return errors.New("seed-endpoints is required for role=agent")
	}
	if c.Role == RoleServer && c.ClusterState == ClusterStateExisting && len(c.SeedEndpoints) == 0 {
		return errors.New("seed-endpoints is required for role=server with cluster-state=existing")
	}
	return nil
}

func envOr(key, fallback string) string {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func envOrInt64(key string, fallback int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	var out int64
	_, err := fmt.Sscan(v, &out)
	if err != nil {
		return fallback
	}
	return out
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
