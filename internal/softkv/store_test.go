package softkv

import (
	"context"
	"testing"
	"time"
)

func TestStorePutGetList(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	_, err := s.Put(ctx, "metrics/node-1", map[string]any{"cpu": 20.5}, 5*time.Second, "node-1")
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}

	entry, ok := s.Get(ctx, "metrics/node-1")
	if !ok {
		t.Fatalf("expected entry exists")
	}
	if entry.SourceID != "node-1" {
		t.Fatalf("unexpected source id: %s", entry.SourceID)
	}

	items := s.List(ctx, "metrics/")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestStoreMerge(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	now := time.Now().UTC()
	current := Entry{Key: "k1", Value: "v1", SourceID: "n1", Seq: 2, UpdatedAt: now.UnixMilli(), ExpiresAt: now.Add(10 * time.Second).UnixMilli()}
	if !s.Merge(ctx, current) {
		t.Fatalf("expected first merge accepted")
	}

	older := Entry{Key: "k1", Value: "old", SourceID: "n1", Seq: 1, UpdatedAt: now.Add(1 * time.Second).UnixMilli(), ExpiresAt: now.Add(10 * time.Second).UnixMilli()}
	if s.Merge(ctx, older) {
		t.Fatalf("expected older seq to be rejected")
	}

	newer := Entry{Key: "k1", Value: "v2", SourceID: "n1", Seq: 3, UpdatedAt: now.Add(1 * time.Second).UnixMilli(), ExpiresAt: now.Add(10 * time.Second).UnixMilli()}
	if !s.Merge(ctx, newer) {
		t.Fatalf("expected newer seq accepted")
	}

	got, ok := s.Get(ctx, "k1")
	if !ok || got.Seq != 3 {
		t.Fatalf("expected seq=3, got %+v", got)
	}
}

func TestStoreMergeRejectsIngestedAtOnlyChange(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	now := time.Now().UTC()
	base := Entry{
		Key:        "k-ingested",
		Value:      "v1",
		SourceID:   "n1",
		Seq:        7,
		UpdatedAt:  now.UnixMilli(),
		ExpiresAt:  now.Add(10 * time.Second).UnixMilli(),
		IngestedAt: now.UnixMilli(),
	}
	if !s.Merge(ctx, base) {
		t.Fatalf("expected first merge accepted")
	}

	// Same version payload except a different ingest timestamp must be rejected.
	replayed := base
	replayed.IngestedAt = now.Add(3 * time.Second).UnixMilli()
	if s.Merge(ctx, replayed) {
		t.Fatalf("expected replay with ingested_at-only change rejected")
	}
}

func TestStoreDeleteExpired(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	_, err := s.Put(ctx, "k-exp", "v", 1*time.Millisecond, "n1")
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}

	time.Sleep(5 * time.Millisecond)
	deleted := s.DeleteExpired(ctx)
	if deleted != 1 {
		t.Fatalf("expected deleted=1, got %d", deleted)
	}
}
