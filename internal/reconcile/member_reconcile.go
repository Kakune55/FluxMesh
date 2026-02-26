package reconcile

import (
	"context"
	"math"
	"sync"
	"time"

	"fluxmesh/internal/logx"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type Task struct {
	MemberID  uint64
	Endpoints []string
	Try       int
	NextAt    time.Time
}

type MemberReconciler struct {
	mu    sync.Mutex
	tasks map[uint64]Task
	tick  time.Duration
}

func NewMemberReconciler() *MemberReconciler {
	return &MemberReconciler{
		tasks: make(map[uint64]Task),
		tick:  time.Second,
	}
}

func (r *MemberReconciler) Add(memberID uint64, endpoints []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[memberID]; exists {
		return
	}
	r.tasks[memberID] = Task{
		MemberID:  memberID,
		Endpoints: append([]string(nil), endpoints...),
		Try:       0,
		NextAt:    time.Now(),
	}
}

func (r *MemberReconciler) Run(ctx context.Context) {
	// 定时扫描待处理任务，按退避时间触发回滚重试。
	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *MemberReconciler) runOnce(ctx context.Context) {
	now := time.Now()
	r.mu.Lock()
	candidates := make([]Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if !task.NextAt.After(now) {
			candidates = append(candidates, task)
		}
	}
	r.mu.Unlock()

	for _, task := range candidates {
		if err := rollbackMember(ctx, task.Endpoints, task.MemberID); err != nil {
			r.scheduleRetry(task, err)
			continue
		}

		r.mu.Lock()
		delete(r.tasks, task.MemberID)
		r.mu.Unlock()
		logx.Info("待协调成员回滚成功", "member_id", task.MemberID)
	}
}

func (r *MemberReconciler) scheduleRetry(task Task, err error) {
	tries := task.Try + 1
	step := 1 << min(tries, 5)
	seconds := int(math.Min(float64(step), 32))
	backoff := time.Duration(seconds) * time.Second

	r.mu.Lock()
	r.tasks[task.MemberID] = Task{
		MemberID:  task.MemberID,
		Endpoints: task.Endpoints,
		Try:       tries,
		NextAt:    time.Now().Add(backoff),
	}
	r.mu.Unlock()

	logx.Warn("待协调成员回滚失败，已安排重试",
		"member_id", task.MemberID,
		"try", tries,
		"next_in", backoff.String(),
		"err", err,
	)
}

func rollbackMember(ctx context.Context, endpoints []string, memberID uint64) error {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer cli.Close()

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = cli.MemberRemove(requestCtx, memberID)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
