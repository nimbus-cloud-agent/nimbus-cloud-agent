package server

import (
	"context"
	"iter"
	"log/slog"
	"sync"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
)

// A batch timer runs only after a notification; idle subscriptions have no
// database timer. Bursts of model frames are read together with low latency.
const changeBatchWindow = 25 * time.Millisecond

type changeMask uint8

const (
	sessionChanged changeMask = 1 << iota
	sandboxChanged
	outboxChanged
	questionsChanged
	eventsChanged
	framesChanged
	allChanges = sessionChanged | sandboxChanged | outboxChanged | questionsChanged | eventsChanged | framesChanged
)

type updateSignal struct {
	Changed   changeMask
	Heartbeat bool
}

// Notifications only invalidate projections. Each subscriber has one wake token
// and a bounded bitmask; a slow browser never blocks PostgreSQL or other clients.
type changeHub struct {
	mu          sync.Mutex
	subscribers map[*subscription]struct{}
	done        chan struct{}
	closed      bool
}
type subscription struct {
	hub       *changeHub
	sessionID string
	interest  changeMask
	pending   changeMask
	wake      chan struct{}
}

func (h *changeHub) subscribe(sessionID string, interest changeMask) *subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done == nil {
		h.done = make(chan struct{})
	}
	if h.subscribers == nil {
		h.subscribers = make(map[*subscription]struct{})
	}
	s := &subscription{hub: h, sessionID: sessionID, interest: interest, pending: interest, wake: make(chan struct{}, 1)}
	if h.closed {
		return s
	}
	h.subscribers[s] = struct{}{}
	s.wake <- struct{}{}
	return s
}
func (h *changeHub) publish(sessionID string, changed changeMask) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subscribers {
		if sessionID != "" && s.sessionID != "" && s.sessionID != sessionID {
			continue
		}
		if relevant := changed & s.interest; relevant != 0 {
			s.pending |= relevant
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
	}
}
func (h *changeHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done == nil {
		h.done = make(chan struct{})
	}
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}
func (s *subscription) close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	delete(s.hub.subscribers, s)
}
func (s *subscription) take() changeMask {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	changed := s.pending
	s.pending = 0
	select {
	case <-s.wake:
	default:
	}
	return changed
}

// Signals yields the initial snapshot request, then coalesced invalidations or
// heartbeat ticks. Heartbeats carry no Changed bits and never cause DB reads.
// The iterator unregisters on break, cancellation, or server shutdown.
func (s *subscription) signals(ctx context.Context, heartbeat time.Duration) iter.Seq[updateSignal] {
	return func(yield func(updateSignal) bool) {
		defer s.close()
		var ticks <-chan time.Time
		if heartbeat > 0 {
			timer := time.NewTicker(heartbeat)
			defer timer.Stop()
			ticks = timer.C
		}
		initial := true
		for {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-s.hub.done:
				return
			default:
			}
			select {
			case <-ctx.Done():
				return
			case <-s.hub.done:
				return
			case <-ticks:
				if !yield(updateSignal{Heartbeat: true}) {
					return
				}
			case <-s.wake:
				if !initial {
					batch := time.NewTimer(changeBatchWindow)
					select {
					case <-ctx.Done():
						batch.Stop()
						return
					case <-s.hub.done:
						batch.Stop()
						return
					case <-batch.C:
					}
				}
				initial = false
				if changed := s.take(); changed != 0 && !yield(updateSignal{Changed: changed}) {
					return
				}
			}
		}
	}
}
func (s *Server) startChanges() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for change, err := range s.S.Changes(s.ctx) {
			if err != nil {
				slog.Warn("database change listener will recover", "error", err)
				continue
			}
			var changed changeMask
			switch change.Kind {
			case store.ChangeResync:
				changed = allChanges
			case store.ChangeSession:
				changed = sessionChanged
			case store.ChangeSandbox:
				changed = sandboxChanged
			case store.ChangeOutbox:
				changed = outboxChanged
			case store.ChangeQuestion:
				changed = questionsChanged
			case store.ChangeEvent:
				changed = eventsChanged
			case store.ChangeFrame:
				changed = framesChanged
			}
			s.updates.publish(change.SessionID, changed)
		}
	}()
}
