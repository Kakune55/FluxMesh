package reconcile

import (
	"context"
	"testing"
	"time"
)

func TestRunReturnsOnCanceledContext(t *testing.T) {
	r := NewMemberReconciler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected Run to exit quickly when context is canceled")
	}
}

func TestRollbackMemberContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rollbackMember(ctx, []string{"http://127.0.0.1:2379"}, 1); err == nil {
		t.Fatalf("expected rollback error with canceled context")
	}
}
