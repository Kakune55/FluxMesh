package etcd

import (
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"fluxmesh/internal/config"
	"fluxmesh/internal/logx"

	"go.etcd.io/etcd/server/v3/embed"
)

type EmbeddedServer struct {
	Etcd *embed.Etcd
}

func StartEmbeddedServer(cfg config.Config, initialCluster string) (*EmbeddedServer, error) {
	// 组装嵌入式 etcd 配置：节点身份、目录与集群拓扑。
	ec := embed.NewConfig()
	ec.Name = cfg.NodeID
	ec.Dir = filepath.Join(cfg.DataDir, cfg.NodeID)
	ec.InitialCluster = initialCluster
	ec.ClusterState = string(cfg.ClusterState)
	ec.LogLevel = "info"

	clientListen, err := parseURL(cfg.ClientListenURL)
	if err != nil {
		return nil, fmt.Errorf("invalid client-listen-url: %w", err)
	}
	clientAdvertise, err := parseURL(cfg.ClientAdvertiseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid client-advertise-url: %w", err)
	}
	peerListen, err := parseURL(cfg.PeerListenURL)
	if err != nil {
		return nil, fmt.Errorf("invalid peer-listen-url: %w", err)
	}
	peerAdvertise, err := parseURL(cfg.PeerAdvertiseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid peer-advertise-url: %w", err)
	}

	ec.ListenClientUrls = []url.URL{clientListen}
	ec.AdvertiseClientUrls = []url.URL{clientAdvertise}
	ec.ListenPeerUrls = []url.URL{peerListen}
	ec.AdvertisePeerUrls = []url.URL{peerAdvertise}

	e, err := embed.StartEtcd(ec)
	if err != nil {
		return nil, err
	}

	select {
	case <-e.Server.ReadyNotify():
		logx.Info("嵌入式 etcd 已就绪", "name", cfg.NodeID)
		return &EmbeddedServer{Etcd: e}, nil
	case <-time.After(30 * time.Second):
		e.Close()
		return nil, fmt.Errorf("embedded etcd start timeout")
	}
}

func (s *EmbeddedServer) Close() {
	if s == nil || s.Etcd == nil {
		return
	}
	s.Etcd.Close()
}

func parseURL(raw string) (url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return url.URL{}, err
	}
	return *u, nil
}
