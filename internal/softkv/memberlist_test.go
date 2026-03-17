package softkv

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestNextJoinRetryIntervalBackoffAndCap(t *testing.T) {
	if got := nextJoinRetryInterval(0, false); got != joinRetryMinInterval {
		t.Fatalf("expected min interval for zero current, got=%s", got)
	}

	if got := nextJoinRetryInterval(joinRetryMinInterval, false); got != 10*time.Second {
		t.Fatalf("expected 10s after first backoff, got=%s", got)
	}

	if got := nextJoinRetryInterval(joinRetryMaxInterval, false); got != joinRetryMaxInterval {
		t.Fatalf("expected capped max interval, got=%s", got)
	}
}

func TestNextJoinRetryIntervalResetOnSuccess(t *testing.T) {
	if got := nextJoinRetryInterval(40*time.Second, true); got != joinRetryMinInterval {
		t.Fatalf("expected reset to min on success, got=%s", got)
	}
}

func TestEncodeDecodeGossipMessage(t *testing.T) {
	original := Event{
		Type: EventPut,
		Entry: Entry{
			Key:       "metrics/nodes/n1",
			Value:     map[string]any{"cpu": 12.5},
			SourceID:  "n1",
			Seq:       3,
			UpdatedAt: 100,
			ExpiresAt: 200,
		},
	}

	raw, err := encodeGossipMessage("n1", original)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := decodeGossipMessage(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Sender != "n1" {
		t.Fatalf("unexpected sender: %s", decoded.Sender)
	}
	if decoded.Event.Type != EventPut {
		t.Fatalf("unexpected type: %s", decoded.Event.Type)
	}
	if decoded.Event.Entry.Key != original.Entry.Key {
		t.Fatalf("unexpected key: %s", decoded.Event.Entry.Key)
	}
}

func TestDecodeGossipMessageRejectEmptySender(t *testing.T) {
	_, err := decodeGossipMessage([]byte(`{"sender":"","event":{"type":"put","entry":{"key":"k"}}}`))
	if err == nil {
		t.Fatal("expected error for empty sender")
	}
}

func TestNormalizeJoinTargets(t *testing.T) {
	out := normalizeJoinTargets([]string{" node-2 ", "node-2:7946", "node-3:9000", ""}, DefaultGossipPort)
	expected := []string{"node-2:7946", "node-3:9000"}
	if !reflect.DeepEqual(out, expected) {
		t.Fatalf("unexpected targets, got=%v expected=%v", out, expected)
	}
}

func TestNormalizeJoinTargetsWithCustomDefaultPort(t *testing.T) {
	out := normalizeJoinTargets([]string{"node-2", "node-3:9000"}, 17946)
	expected := []string{"node-2:17946", "node-3:9000"}
	if !reflect.DeepEqual(out, expected) {
		t.Fatalf("unexpected targets, got=%v expected=%v", out, expected)
	}
}

func TestGossipDelegateNotifyMsgMerge(t *testing.T) {
	store := NewStore()
	ctx := context.Background()
	baseNow := time.Now().UTC()

	_, err := store.Put(ctx, "metrics/nodes/node-1", map[string]any{"cpu": 10.0}, 10*time.Second, "node-1")
	if err != nil {
		t.Fatalf("seed put failed: %v", err)
	}

	delegate := &gossipDelegate{store: store, nodeID: "node-2"}
	incoming := Event{
		Type: EventPut,
		Entry: Entry{
			Key:       "metrics/nodes/node-1",
			Value:     map[string]any{"cpu": 90.0},
			SourceID:  "node-1",
			Seq:       999,
			UpdatedAt: baseNow.Add(1 * time.Second).UnixMilli(),
			ExpiresAt: baseNow.Add(10 * time.Second).UnixMilli(),
		},
	}

	raw, err := encodeGossipMessage("node-1", incoming)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	delegate.NotifyMsg(raw)

	entry, ok := store.Get(ctx, "metrics/nodes/node-1")
	if !ok {
		t.Fatal("expected merged entry to exist")
	}
	if entry.Seq != 999 {
		t.Fatalf("expected seq=999, got %d", entry.Seq)
	}
}

func TestGossipDelegateLocalStateMergeRemoteState(t *testing.T) {
	source := NewStore()
	sink := NewStore()

	ctx := context.Background()
	_, err := source.Put(ctx, "metrics/nodes/node-a", map[string]any{"cpu": 11.0}, 10*time.Second, "node-a")
	if err != nil {
		t.Fatalf("source put failed: %v", err)
	}

	srcDelegate := &gossipDelegate{store: source, nodeID: "node-a"}
	raw := srcDelegate.LocalState(false)
	if len(raw) == 0 {
		t.Fatal("expected local state payload")
	}

	sinkDelegate := &gossipDelegate{store: sink, nodeID: "node-b"}
	sinkDelegate.MergeRemoteState(raw, false)

	entry, ok := sink.Get(ctx, "metrics/nodes/node-a")
	if !ok {
		t.Fatal("expected merged entry in sink store")
	}
	if entry.SourceID != "node-a" {
		t.Fatalf("unexpected source id: %s", entry.SourceID)
	}
}

func TestResolveAdvertiseAddrIP(t *testing.T) {
	out, err := resolveAdvertiseAddr("10.10.10.10")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if out != "10.10.10.10" {
		t.Fatalf("unexpected resolved ip: %s", out)
	}
}

func TestResolveAdvertiseAddrHostname(t *testing.T) {
	out, err := resolveAdvertiseAddr("localhost")
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if out == "" {
		t.Fatal("expected resolved ip, got empty")
	}
}
