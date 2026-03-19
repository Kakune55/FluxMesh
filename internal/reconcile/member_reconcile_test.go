package reconcile

import (
	"context"
	"testing"
	"time"
)

func TestNewMemberReconcilerDefaults(t *testing.T) {
	r := NewMemberReconciler()
	if r == nil {
		t.Fatalf("expected non-nil reconciler")
	}
	if r.tasks == nil {
		t.Fatalf("expected tasks map initialized")
	}
	if r.tick != time.Second {
		t.Fatalf("expected default tick 1s, got %v", r.tick)
	}
}

func TestAddNoDuplicateAndCopyEndpoints(t *testing.T) {
	r := NewMemberReconciler()

	ep := []string{"127.0.0.1:2379", "127.0.0.1:2380"}
	r.Add(1001, ep)

	item, ok := r.tasks[1001]
	if !ok {
		t.Fatalf("expected task for member 1001")
	}
	if item.Try != 0 {
		t.Fatalf("expected try=0, got %d", item.Try)
	}
	if len(item.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(item.Endpoints))
	}

	ep[0] = "mutated"
	if item.Endpoints[0] != "127.0.0.1:2379" {
		t.Fatalf("expected endpoints copied on add, got %v", item.Endpoints)
	}

	firstNext := item.NextAt
	r.Add(1001, []string{"10.0.0.1:2379"})
	item2 := r.tasks[1001]
	if len(item2.Endpoints) != 2 {
		t.Fatalf("expected duplicate add ignored, got endpoints=%v", item2.Endpoints)
	}
	if !item2.NextAt.Equal(firstNext) {
		t.Fatalf("expected duplicate add not overwrite task")
	}
}

func TestScheduleRetryBackoffAndCap(t *testing.T) {
	r := NewMemberReconciler()

	start := time.Now()
	r.scheduleRetry(Task{MemberID: 2001, Endpoints: []string{"127.0.0.1:2379"}, Try: 0}, context.DeadlineExceeded)
	item := r.tasks[2001]
	if item.Try != 1 {
		t.Fatalf("expected try=1, got %d", item.Try)
	}
	backoff := item.NextAt.Sub(start)
	if backoff < 2*time.Second || backoff > 3*time.Second {
		t.Fatalf("expected about 2s backoff, got %v", backoff)
	}

	start = time.Now()
	r.scheduleRetry(Task{MemberID: 2002, Endpoints: []string{"127.0.0.1:2379"}, Try: 8}, context.DeadlineExceeded)
	item = r.tasks[2002]
	if item.Try != 9 {
		t.Fatalf("expected try=9, got %d", item.Try)
	}
	backoff = item.NextAt.Sub(start)
	if backoff < 32*time.Second || backoff > 33*time.Second {
		t.Fatalf("expected capped backoff about 32s, got %v", backoff)
	}
}

func TestRunOnceRetryDueTaskOnly(t *testing.T) {
	r := NewMemberReconciler()
	now := time.Now()
	pendingNext := now.Add(time.Hour)

	r.tasks[3001] = Task{
		MemberID:  3001,
		Endpoints: nil,
		Try:       0,
		NextAt:    now.Add(-time.Second),
	}
	r.tasks[3002] = Task{
		MemberID:  3002,
		Endpoints: nil,
		Try:       3,
		NextAt:    pendingNext,
	}

	r.runOnce(context.Background())

	due := r.tasks[3001]
	if due.Try != 1 {
		t.Fatalf("expected due task retried to try=1, got %d", due.Try)
	}
	if due.NextAt.Before(time.Now()) {
		t.Fatalf("expected due task scheduled in future, got next_at=%v", due.NextAt)
	}

	pending := r.tasks[3002]
	if pending.Try != 3 {
		t.Fatalf("expected not-due task unchanged, got try=%d", pending.Try)
	}
	if !pending.NextAt.Equal(pendingNext) {
		t.Fatalf("expected not-due task next_at unchanged")
	}
}

func TestMin(t *testing.T) {
	if got := min(1, 2); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
	if got := min(3, -1); got != -1 {
		t.Fatalf("expected -1, got %d", got)
	}
	if got := min(5, 5); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}
