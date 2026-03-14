package softkv

import (
	"context"
	"errors"
	"time"
)

const DefaultPublishTimeout = 200 * time.Millisecond

type WriteResult struct {
	Entry      Entry
	PublishErr error
}

type Writer struct {
	store          *Store
	bus            *Bus
	publishTimeout time.Duration
}

func NewWriter(store *Store, bus *Bus) *Writer {
	return &Writer{store: store, bus: bus, publishTimeout: DefaultPublishTimeout}
}

func (w *Writer) SetPublishTimeout(timeout time.Duration) {
	if timeout <= 0 {
		w.publishTimeout = DefaultPublishTimeout
		return
	}
	w.publishTimeout = timeout
}

func (w *Writer) Write(ctx context.Context, key string, value any, ttl time.Duration, sourceID string) (WriteResult, error) {
	if w == nil || w.store == nil {
		return WriteResult{}, errors.New("nil softkv writer")
	}

	entry, err := w.store.Put(ctx, key, value, ttl, sourceID)
	if err != nil {
		return WriteResult{}, err
	}

	if w.bus == nil {
		return WriteResult{Entry: entry}, nil
	}

	pubCtx, cancel := context.WithTimeout(ctx, w.publishTimeout)
	pubErr := w.bus.Publish(pubCtx, Event{Type: EventPut, Entry: entry})
	cancel()

	return WriteResult{Entry: entry, PublishErr: pubErr}, nil
}
