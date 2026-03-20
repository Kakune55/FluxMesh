package softkv

import (
	"context"
	"testing"
	"time"
)

func TestWriterSetPublishTimeout(t *testing.T) {
	w := NewWriter(NewStore(), NewBus(1))

	w.SetPublishTimeout(123 * time.Millisecond)
	if w.publishTimeout != 123*time.Millisecond {
		t.Fatalf("expected custom timeout, got %v", w.publishTimeout)
	}

	w.SetPublishTimeout(0)
	if w.publishTimeout != DefaultPublishTimeout {
		t.Fatalf("expected default timeout, got %v", w.publishTimeout)
	}
}

func TestStoreStats(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	_, err := s.Put(ctx, "k1", "v1", 10*time.Second, "n1")
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}
	_, err = s.Put(ctx, "k2", "v2", 10*time.Second, "n1")
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}

	stats := s.Stats()
	if stats.LiveEntries != 2 {
		t.Fatalf("expected live_entries=2, got %d", stats.LiveEntries)
	}
	if stats.PutTotal < 2 {
		t.Fatalf("expected put_total>=2, got %d", stats.PutTotal)
	}
}

func TestGossipBroadcastMethods(t *testing.T) {
	b := &gossipBroadcast{msg: []byte("hello")}
	if b.Invalidates(nil) {
		t.Fatalf("expected Invalidates=false")
	}
	if string(b.Message()) != "hello" {
		t.Fatalf("expected message hello, got %q", string(b.Message()))
	}
	b.Finished()
}

func TestGossipDelegateMetaAndBroadcasts(t *testing.T) {
	d := &gossipDelegate{}
	if meta := d.NodeMeta(1024); meta != nil {
		t.Fatalf("expected nil node meta, got %v", meta)
	}
	if out := d.GetBroadcasts(10, 1024); out != nil {
		t.Fatalf("expected nil broadcasts when queue nil, got %v", out)
	}
}

func TestRunMemberlistValidation(t *testing.T) {
	err := RunMemberlist(context.Background(), nil, make(chan Event), MemberlistOptions{NodeID: "n1"})
	if err == nil || err.Error() != "nil store" {
		t.Fatalf("expected nil store error, got %v", err)
	}

	err = RunMemberlist(context.Background(), NewStore(), nil, MemberlistOptions{NodeID: "n1"})
	if err == nil || err.Error() != "nil events" {
		t.Fatalf("expected nil events error, got %v", err)
	}

	err = RunMemberlist(context.Background(), NewStore(), make(chan Event), MemberlistOptions{})
	if err == nil || err.Error() != "empty node id" {
		t.Fatalf("expected empty node id error, got %v", err)
	}
}
