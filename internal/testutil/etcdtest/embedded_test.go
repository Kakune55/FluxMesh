package etcdtest

import (
	"context"
	"strings"
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestMustAllocURL(t *testing.T) {
	u := mustAllocURL(t)
	if u == nil {
		t.Fatalf("expected non-nil url")
	}
	if u.Scheme != "http" {
		t.Fatalf("expected http scheme, got %s", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		t.Fatalf("expected host")
	}
}

func TestStartEmbeddedEtcd(t *testing.T) {
	emb := Start(t)
	if emb == nil || emb.Client == nil || emb.Server == nil {
		t.Fatalf("expected embedded etcd resources to be initialized")
	}

	ctx := context.Background()
	_, err := emb.Client.Put(ctx, "test-key", "test-value")
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}

	resp, err := emb.Client.Get(ctx, "test-key", clientv3.WithSerializable())
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(resp.Kvs) != 1 || string(resp.Kvs[0].Value) != "test-value" {
		t.Fatalf("unexpected kv response: %+v", resp.Kvs)
	}
}
