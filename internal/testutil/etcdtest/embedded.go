package etcdtest

import (
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

type Embedded struct {
	Client *clientv3.Client
	Server *embed.Etcd
}

func Start(t *testing.T) *Embedded {
	t.Helper()

	clientURL := mustAllocURL(t)
	peerURL := mustAllocURL(t)

	ec := embed.NewConfig()
	ec.Name = "test-node"
	ec.Dir = t.TempDir()
	ec.LogLevel = "error"
	ec.Logger = "zap"
	ec.ClusterState = "new"
	ec.InitialCluster = fmt.Sprintf("%s=%s", ec.Name, peerURL.String())
	ec.ListenClientUrls = []url.URL{*clientURL}
	ec.AdvertiseClientUrls = []url.URL{*clientURL}
	ec.ListenPeerUrls = []url.URL{*peerURL}
	ec.AdvertisePeerUrls = []url.URL{*peerURL}

	srv, err := embed.StartEtcd(ec)
	if err != nil {
		t.Fatalf("failed to start embedded etcd: %v", err)
	}

	select {
	case <-srv.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		srv.Close()
		t.Fatalf("embedded etcd start timeout")
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{clientURL.String()},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		srv.Close()
		t.Fatalf("failed to create etcd client: %v", err)
	}

	t.Cleanup(func() {
		_ = cli.Close()
		srv.Close()
	})

	return &Embedded{Client: cli, Server: srv}
}

func mustAllocURL(t *testing.T) *url.URL {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	u, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatalf("failed to parse url: %v", err)
	}
	return u
}
