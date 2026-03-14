package softkv

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWriterWriteSuccess(t *testing.T) {
	store := NewStore()
	bus := NewBus(8)
	writer := NewWriter(store, bus)

	res, err := writer.Write(context.Background(), "metrics/nodes/n1", map[string]any{"cpu": 11.1}, 10*time.Second, "n1")
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if res.Entry.Key != "metrics/nodes/n1" {
		t.Fatalf("unexpected key: %s", res.Entry.Key)
	}
	if res.PublishErr != nil {
		t.Fatalf("unexpected publish err: %v", res.PublishErr)
	}

	got, ok := store.Get(context.Background(), "metrics/nodes/n1")
	if !ok {
		t.Fatal("expected entry in store")
	}
	if got.SourceID != "n1" {
		t.Fatalf("unexpected source id: %s", got.SourceID)
	}
}

func TestWriterWritePublishErrorDoesNotFailWrite(t *testing.T) {
	store := NewStore()
	bus := NewBus(1)
	writer := NewWriter(store, bus)

	if err := bus.Publish(context.Background(), Event{Type: EventPut, Entry: Entry{Key: "pre-fill"}}); err != nil {
		t.Fatalf("pre-fill bus failed: %v", err)
	}

	res, err := writer.Write(context.Background(), "metrics/nodes/n2", map[string]any{"cpu": 22.2}, 10*time.Second, "n2")
	if err != nil {
		t.Fatalf("write should not fail on publish error: %v", err)
	}
	if !errors.Is(res.PublishErr, ErrBusFull) {
		t.Fatalf("expected ErrBusFull, got %v", res.PublishErr)
	}

	if _, ok := store.Get(context.Background(), "metrics/nodes/n2"); !ok {
		t.Fatal("expected entry in store")
	}
}
