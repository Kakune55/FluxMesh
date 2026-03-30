package traffic

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"fluxmesh/internal/model"
	"fluxmesh/internal/registry"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

func mustAllocHTTPURL(t *testing.T) url.URL {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate tcp addr failed: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	u, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatalf("parse url failed: %v", err)
	}
	return *u
}

func startEmbeddedClientWithRetry(t *testing.T) *clientv3.Client {
	t.Helper()

	for attempt := 0; attempt < 5; attempt++ {
		clientURL := mustAllocHTTPURL(t)
		peerURL := mustAllocHTTPURL(t)

		ec := embed.NewConfig()
		ec.Name = "traffic-test-node"
		ec.Dir = t.TempDir()
		ec.LogLevel = "error"
		ec.Logger = "zap"
		ec.ClusterState = "new"
		ec.InitialCluster = fmt.Sprintf("%s=%s", ec.Name, peerURL.String())
		ec.ListenClientUrls = []url.URL{clientURL}
		ec.AdvertiseClientUrls = []url.URL{clientURL}
		ec.ListenPeerUrls = []url.URL{peerURL}
		ec.AdvertisePeerUrls = []url.URL{peerURL}

		srv, err := embed.StartEtcd(ec)
		if err != nil {
			if strings.Contains(err.Error(), "address already in use") {
				continue
			}
			t.Fatalf("start embedded etcd failed: %v", err)
		}

		select {
		case <-srv.Server.ReadyNotify():
		case <-time.After(10 * time.Second):
			srv.Close()
			t.Fatalf("embedded etcd ready timeout")
		}

		cli, err := clientv3.New(clientv3.Config{Endpoints: []string{clientURL.String()}, DialTimeout: 5 * time.Second})
		if err != nil {
			srv.Close()
			if strings.Contains(err.Error(), "connection refused") {
				continue
			}
			t.Fatalf("create etcd client failed: %v", err)
		}

		t.Cleanup(func() {
			_ = cli.Close()
			srv.Close()
		})
		return cli
	}

	t.Fatalf("failed to start embedded etcd after retries")
	return nil
}

func reservePort(t *testing.T, network string) int {
	t.Helper()
	ln, err := net.Listen(network, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve %s port failed: %v", network, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestServerRefreshAndStartIntegration(t *testing.T) {
	cli := startEmbeddedClientWithRetry(t)
	services := registry.NewServices(cli)

	httpPort := reservePort(t, "tcp")
	tcpPort := reservePort(t, "tcp")
	udpPort := reservePort(t, "tcp")

	cfgs := []model.ServiceConfig{
		{
			Name: "http-svc-int",
			TrafficPolicy: model.ServiceTrafficPolicy{Listener: model.ListenerPolicy{Addr: "127.0.0.1", Port: httpPort}},
			Routes:        []model.ServiceRoute{{Hosts: []string{"*"}, PathPrefix: "/", Destination: "127.0.0.1:28080", Weight: 100}},
		},
		{
			Name: "tcp-svc-int",
			TrafficPolicy: model.ServiceTrafficPolicy{Proxy: model.ProxyPolicy{Layer: "l4-tcp"}, Listener: model.ListenerPolicy{Addr: "127.0.0.1", Port: tcpPort}},
			Routes:        []model.ServiceRoute{{Hosts: []string{"*"}, PathPrefix: "/", Destination: "127.0.0.1:3306", Weight: 100}},
		},
		{
			Name: "udp-svc-int",
			TrafficPolicy: model.ServiceTrafficPolicy{Proxy: model.ProxyPolicy{Layer: "l4-udp"}, Listener: model.ListenerPolicy{Addr: "127.0.0.1", Port: udpPort}},
			Routes:        []model.ServiceRoute{{Hosts: []string{"*"}, PathPrefix: "/", Destination: "127.0.0.1:5353", Weight: 100}},
		},
	}

	for _, cfg := range cfgs {
		if err := services.Put(context.Background(), cfg); err != nil {
			t.Fatalf("put service %s failed: %v", cfg.Name, err)
		}
	}

	s := NewServer(services)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	time.Sleep(120 * time.Millisecond)

	s.mu.RLock()
	httpCount := len(s.listeners)
	tcpCount := len(s.tcpListeners)
	udpCount := len(s.udpListeners)
	s.mu.RUnlock()
	if httpCount == 0 || tcpCount == 0 || udpCount == 0 {
		t.Fatalf("expected listeners created by refresh, got http=%d tcp=%d udp=%d", httpCount, tcpCount, udpCount)
	}

	for _, name := range []string{"http-svc-int", "tcp-svc-int", "udp-svc-int"} {
		if err := services.Delete(context.Background(), name); err != nil {
			t.Fatalf("delete service %s failed: %v", name, err)
		}
	}
	if err := s.refresh(context.Background()); err != nil {
		t.Fatalf("refresh after delete failed: %v", err)
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}
