package softkv

import (
	"context"
	"errors"
)

var ErrBusFull = errors.New("softkv bus is full")

type EventType string

const (
	EventPut EventType = "put"
)

type Event struct {
	Type  EventType `json:"type"`
	Entry Entry     `json:"entry"`
}

type Bus struct {
	ch chan Event
}

func NewBus(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 256
	}
	return &Bus{ch: make(chan Event, buffer)}
}

func (b *Bus) Publish(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case b.ch <- event:
		return nil
	default:
		return ErrBusFull
	}
}

func (b *Bus) Subscribe() <-chan Event {
	return b.ch
}

func RunLoopback(ctx context.Context, store *Store, events <-chan Event) {
	if store == nil || events == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if event.Type != EventPut {
				continue
			}
			store.Merge(ctx, event.Entry)
		}
	}
}
