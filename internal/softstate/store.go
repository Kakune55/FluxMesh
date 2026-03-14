package softstate

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
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
	if key == "" {
		return Entry{}, ErrInvalidKey
	}
	if ttl <= 0 {
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
	if key == "" {
		return Entry{}, false
	}

	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		return Entry{}, false
	}
	if entry.ExpiresAt <= s.now().UTC().UnixMilli() {
		return Entry{}, false
	}
	return entry, true
}

func (s *Store) List(_ context.Context, prefix string) []Entry {
	prefix = strings.TrimSpace(prefix)
	nowMs := s.now().UTC().UnixMilli()

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
	if strings.TrimSpace(incoming.Key) == "" {
		return false
	}
	if incoming.ExpiresAt <= s.now().UTC().UnixMilli() {
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
		return true
	}

	if shouldAccept(current, incoming) {
		s.data[incoming.Key] = incoming
		if incoming.SourceID != "" && incoming.Seq > s.seq[incoming.SourceID] {
			s.seq[incoming.SourceID] = incoming.Seq
		}
		return true
	}
	return false
}

func (s *Store) DeleteExpired(_ context.Context) int {
	nowMs := s.now().UTC().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for key, entry := range s.data {
		if entry.ExpiresAt <= nowMs {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
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
