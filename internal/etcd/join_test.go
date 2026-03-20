package etcd

import (
	"testing"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
)

func TestBuildInitialCluster(t *testing.T) {
	nodeName := "node-c"
	peerURL := "http://10.0.0.3:2380"

	members := []*etcdserverpb.Member{
		{Name: "node-a", PeerURLs: []string{"http://10.0.0.1:2380", "http://10.0.0.1:1234"}},
		{Name: "", PeerURLs: []string{peerURL}},
		{Name: "", PeerURLs: []string{"http://10.0.0.4:2380"}},
		{Name: "node-d", PeerURLs: nil},
		{Name: "node-e", PeerURLs: []string{"http://10.0.0.5:2380"}},
	}

	got := buildInitialCluster(members, nodeName, peerURL)
	want := "node-a=http://10.0.0.1:2380,node-c=http://10.0.0.3:2380,node-e=http://10.0.0.5:2380"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildInitialClusterEmpty(t *testing.T) {
	got := buildInitialCluster([]*etcdserverpb.Member{
		{Name: "", PeerURLs: nil},
		{Name: "", PeerURLs: []string{"http://10.0.0.1:2380"}},
	}, "node-x", "http://10.0.0.9:2380")
	if got != "" {
		t.Fatalf("expected empty cluster string, got %q", got)
	}
}

func TestContainsURL(t *testing.T) {
	urls := []string{"http://a:1", "http://b:2"}
	if !containsURL(urls, "http://b:2") {
		t.Fatalf("expected url to be found")
	}
	if containsURL(urls, "http://c:3") {
		t.Fatalf("expected url not to be found")
	}
}
