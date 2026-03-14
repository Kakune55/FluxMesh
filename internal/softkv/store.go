package softkv

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrInvalidKey = errors.New("invalid key")
var ErrInvalidTTL = errors.New("ttl must be > 0")

type Entry struct {
	Key       string `json:"key"`
	Value     any    `json:"value"`
	SourceID  string `json:"source_id"`
	Seq       uint64 `json:"seq"`
	UpdatedAt int64  `json:"updated_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Entry
	seq  map[string]uint64
	now  func() time.Time
	stats storeCounters
}

type storeCounters struct {
	putTotal          atomic.Uint64
	putErrors         atomic.Uint64
	getTotal          atomic.Uint64
	getHits           atomic.Uint64
	getMisses         atomic.Uint64
	listTotal         atomic.Uint64
	mergeTotal        atomic.Uint64
	mergeAccepted     atomic.Uint64
	mergeRejected     atomic.Uint64
	deleteExpiredRuns atomic.Uint64
	deleteExpiredKeys atomic.Uint64
}

type StoreStats struct {
	PutTotal            uint64 `json:"put_total"`
	PutErrors           uint64 `json:"put_errors"`
	GetTotal            uint64 `json:"get_total"`
	GetHits             uint64 `json:"get_hits"`
	GetMisses           uint64 `json:"get_misses"`
	ListTotal           uint64 `json:"list_total"`
	MergeTotal          uint64 `json:"merge_total"`
	MergeAccepted       uint64 `json:"merge_accepted"`
	MergeRejected       uint64 `json:"merge_rejected"`
	DeleteExpiredRuns   uint64 `json:"delete_expired_runs"`
	DeleteExpiredKeys   uint64 `json:"delete_expired_keys"`
	LiveEntries         int    `json:"live_entries"`
	Sources             int    `json:"sources"`
	SnapshotAtUnixMilli int64  `json:"snapshot_at_unix_milli"`
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]Entry),
		seq:  make(map[string]uint64),
		now:  time.Now,
	}
}

func (s *Store) Put(_ context.Context, key string, value any, ttl time.Duration, sourceID string) (Entry, error) {
	key = strings.TrimSpace(key)
	s.stats.putTotal.Add(1)
	if key == "" {
		s.stats.putErrors.Add(1)
		return Entry{}, ErrInvalidKey
	}
	if ttl <= 0 {
		s.stats.putErrors.Add(1)
		return Entry{}, ErrInvalidTTL
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq[sourceID]++
	now := s.now().UTC()
	entry := Entry{
		Key:       key,
		Value:     value,
		SourceID:  sourceID,
		Seq:       s.seq[sourceID],
		UpdatedAt: now.UnixMilli(),
		ExpiresAt: now.Add(ttl).UnixMilli(),
	}
	s.data[key] = entry
	return entry, nil
}

func (s *Store) Get(_ context.Context, key string) (Entry, bool) {
	key = strings.TrimSpace(key)
	s.stats.getTotal.Add(1)
	if key == "" {
		s.stats.getMisses.Add(1)
		return Entry{}, false
	}

	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()

	if !ok {
		s.stats.getMisses.Add(1)
		return Entry{}, false
	}
	if entry.ExpiresAt <= s.now().UTC().UnixMilli() {
		s.stats.getMisses.Add(1)
		return Entry{}, false
	}
	s.stats.getHits.Add(1)
	return entry, true
}

func (s *Store) List(_ context.Context, prefix string) []Entry {
	prefix = strings.TrimSpace(prefix)
	nowMs := s.now().UTC().UnixMilli()
	s.stats.listTotal.Add(1)

	s.mu.RLock()
	items := make([]Entry, 0, len(s.data))
	for _, entry := range s.data {
		if entry.ExpiresAt <= nowMs {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.Key, prefix) {
			continue
		}
		items = append(items, entry)
	}
	s.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})
	return items
}

func (s *Store) Merge(_ context.Context, incoming Entry) bool {
	s.stats.mergeTotal.Add(1)
	if strings.TrimSpace(incoming.Key) == "" {
		s.stats.mergeRejected.Add(1)
		return false
	}
	if incoming.ExpiresAt <= s.now().UTC().UnixMilli() {
		s.stats.mergeRejected.Add(1)
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.data[incoming.Key]
	if !exists {
		s.data[incoming.Key] = incoming
		if incoming.SourceID != "" && incoming.Seq > s.seq[incoming.SourceID] {
			s.seq[incoming.SourceID] = incoming.Seq
		}
		s.stats.mergeAccepted.Add(1)
		return true
	}

	if shouldAccept(current, incoming) {
		s.data[incoming.Key] = incoming
		if incoming.SourceID != "" && incoming.Seq > s.seq[incoming.SourceID] {
			s.seq[incoming.SourceID] = incoming.Seq
		}
		s.stats.mergeAccepted.Add(1)
		return true
	}
	s.stats.mergeRejected.Add(1)
	return false
}

func (s *Store) DeleteExpired(_ context.Context) int {
	nowMs := s.now().UTC().UnixMilli()
	s.stats.deleteExpiredRuns.Add(1)

	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for key, entry := range s.data {
		if entry.ExpiresAt <= nowMs {
			delete(s.data, key)
			deleted++
		}
	}
	s.stats.deleteExpiredKeys.Add(uint64(deleted))
	return deleted
}

func (s *Store) Stats() StoreStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := StoreStats{
		PutTotal:          s.stats.putTotal.Load(),
		PutErrors:         s.stats.putErrors.Load(),
		GetTotal:          s.stats.getTotal.Load(),
		GetHits:           s.stats.getHits.Load(),
		GetMisses:         s.stats.getMisses.Load(),
		ListTotal:         s.stats.listTotal.Load(),
		MergeTotal:        s.stats.mergeTotal.Load(),
		MergeAccepted:     s.stats.mergeAccepted.Load(),
		MergeRejected:     s.stats.mergeRejected.Load(),
		DeleteExpiredRuns: s.stats.deleteExpiredRuns.Load(),
		DeleteExpiredKeys: s.stats.deleteExpiredKeys.Load(),
	}
	stats.LiveEntries = len(s.data)
	stats.Sources = len(s.seq)
	stats.SnapshotAtUnixMilli = s.now().UTC().UnixMilli()
	return stats
}

func shouldAccept(current Entry, incoming Entry) bool {
	if current.SourceID == incoming.SourceID {
		if incoming.Seq != current.Seq {
			return incoming.Seq > current.Seq
		}
	}
	if incoming.UpdatedAt != current.UpdatedAt {
		return incoming.UpdatedAt > current.UpdatedAt
	}
	if incoming.SourceID != current.SourceID {
		return incoming.SourceID > current.SourceID
	}
	return incoming.Seq > current.Seq
}
