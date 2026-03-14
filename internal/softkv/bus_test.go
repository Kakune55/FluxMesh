package softkv

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBusPublishFull(t *testing.T) {
	bus := NewBus(1)
	ctx := context.Background()

	err := bus.Publish(ctx, Event{Type: EventPut, Entry: Entry{Key: "k1"}})
	if err != nil {
		t.Fatalf("first publish should succeed: %v", err)
	}

	err = bus.Publish(ctx, Event{Type: EventPut, Entry: Entry{Key: "k2"}})
	if !errors.Is(err, ErrBusFull) {
		t.Fatalf("expected ErrBusFull, got %v", err)
	}
}

func TestRunLoopbackMerge(t *testing.T) {
	store := NewStore()
	bus := NewBus(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go RunLoopback(ctx, store, bus.Subscribe())

	baseNow := time.Now().UTC()
	_, err := store.Put(ctx, "metrics/nodes/node-1", map[string]any{"cpu": 10.0}, 10*time.Second, "node-1")
	if err != nil {
		t.Fatalf("seed put failed: %v", err)
	}

	incoming := Entry{
		Key:       "metrics/nodes/node-1",
		Value:     map[string]any{"cpu": 88.8},
		SourceID:  "node-1",
		Seq:       99,
		UpdatedAt: baseNow.Add(1 * time.Second).UnixMilli(),
		ExpiresAt: baseNow.Add(30 * time.Second).UnixMilli(),
	}

	err = bus.Publish(ctx, Event{Type: EventPut, Entry: incoming})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		entry, ok := store.Get(ctx, "metrics/nodes/node-1")
		if ok && entry.Seq == 99 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected merged seq=99 not observed in time")
}
