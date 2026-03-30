package softkv

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func reservePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port failed: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestRunMemberlistStartAndCancel(t *testing.T) {
	store := NewStore()
	events := make(chan Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := MemberlistOptions{
		NodeID:        "node-softkv-a",
		BindAddr:      "127.0.0.1",
		BindPort:      reservePort(t),
		AdvertiseAddr: "127.0.0.1",
		Join:          []string{"127.0.0.1:1"},
	}

	done := make(chan error, 1)
	go func() {
		done <- RunMemberlist(ctx, store, events, opts)
	}()

	events <- Event{Type: EventPut, Entry: Entry{Key: "k1", Value: "v1", SourceID: "s1", Seq: 1, UpdatedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(10 * time.Second).UnixMilli()}}
	events <- Event{Type: EventType("unknown")}

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error on context cancel, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("run memberlist did not exit in time")
	}
}

func TestStoreGetAndPutErrorBranches(t *testing.T) {
	s := NewStore()
	ctx := context.Background()

	if _, ok := s.Get(ctx, ""); ok {
		t.Fatalf("expected empty key get miss")
	}
	if _, ok := s.Get(ctx, "not-found"); ok {
		t.Fatalf("expected not-found get miss")
	}

	if _, err := s.Put(ctx, "", "v", time.Second, "n1"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
	if _, err := s.Put(ctx, "k", "v", 0, "n1"); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("expected ErrInvalidTTL, got %v", err)
	}

	_, err := s.Put(ctx, "k-exp", "v", 5*time.Millisecond, "n1")
	if err != nil {
		t.Fatalf("put should succeed, got %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, ok := s.Get(ctx, "k-exp"); ok {
		t.Fatalf("expected expired entry get miss")
	}
}

func TestWriterNilAndNoBusBranches(t *testing.T) {
	var nilWriter *Writer
	if _, err := nilWriter.Write(context.Background(), "k", "v", time.Second, "n1"); err == nil {
		t.Fatalf("expected nil writer error")
	}

	writer := NewWriter(NewStore(), nil)
	res, err := writer.Write(context.Background(), "k2", "v2", time.Second, "n1")
	if err != nil {
		t.Fatalf("expected write without bus success, got %v", err)
	}
	if res.PublishErr != nil {
		t.Fatalf("expected nil publish error when bus is nil, got %v", res.PublishErr)
	}
}

func TestBusDefaultsAndCanceledPublish(t *testing.T) {
	bus := NewBus(0)
	if bus == nil || bus.ch == nil {
		t.Fatalf("expected default bus channel initialized")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := bus.Publish(ctx, Event{Type: EventPut})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected nil or context canceled due select race, got %v", err)
	}
}
