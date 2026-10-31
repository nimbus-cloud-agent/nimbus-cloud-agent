package server

import (
	"context"
	"iter"
	"testing"
	"time"
)

func TestChangeSubscriptionsCoalesceAndFilter(t *testing.T) {
	var hub changeHub
	a := hub.subscribe("a", allChanges)
	list := hub.subscribe("", sessionChanged|sandboxChanged)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next, stop := iter.Pull(a.signals(ctx, 0))
	defer stop()
	initial, ok := next()
	if !ok || initial.Changed != allChanges {
		t.Fatal(initial, ok)
	}
	listNext, listStop := iter.Pull(list.signals(ctx, 0))
	defer listStop()
	listNext()
	// Keep both consumers stalled while producers publish a large burst.
	for range 10000 {
		hub.publish("a", framesChanged)
		hub.publish("b", questionsChanged)
	}
	hub.publish("a", eventsChanged)
	got, ok := next()
	if !ok || got.Changed != framesChanged|eventsChanged {
		t.Fatal(got, ok)
	}
	if len(a.wake) != 0 || len(list.wake) != 0 {
		t.Fatal("unrelated/list subscriptions woke or burst was unbounded")
	}
	hub.publish("", allChanges) // reconnect invalidates every projection
	got, _ = next()
	if got.Changed != allChanges {
		t.Fatal(got)
	}
	got, _ = listNext()
	if got.Changed != sessionChanged|sandboxChanged {
		t.Fatal(got)
	}
	stop()
	listStop()
	if len(hub.subscribers) != 0 {
		t.Fatal("iterator break leaked subscription")
	}
}
func TestHeartbeatDoesNotInvalidateAndCancellationClosesIterator(t *testing.T) {
	var hub changeHub
	sub := hub.subscribe("a", allChanges)
	ctx, cancel := context.WithCancel(context.Background())
	next, stop := iter.Pull(sub.signals(ctx, 10*time.Millisecond))
	defer stop()
	next()
	signal, ok := next()
	if !ok || !signal.Heartbeat || signal.Changed != 0 {
		t.Fatal(signal, ok)
	}
	cancel()
	if _, ok := next(); ok {
		t.Fatal("cancelled iterator yielded")
	}
	if len(hub.subscribers) != 0 {
		t.Fatal("cancelled subscription leaked")
	}
	sub = hub.subscribe("a", allChanges)
	hub.close()
	for range sub.signals(context.Background(), 0) {
		t.Fatal("closed hub yielded")
	}
}
func startTestChanges(t *testing.T, s *Server) {
	t.Helper()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.startChanges()
	t.Cleanup(func() { s.cancel(); s.updates.close(); s.wg.Wait() })
}
