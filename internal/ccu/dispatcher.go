// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"context"
	"sync"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// Asynchronous, batched event delivery.
//
// The simulator delivers each event as its own synchronous XML-RPC call
// on the goroutine that produced it, so a callback receiver that is
// slow to answer stalls the setValue that caused the event — up to the
// client timeout, per event.
//
// A CCU does neither: it hands events to a dispatcher thread, and that
// thread bundles whatever has accumulated into a single
// `system.multicall` with one `event` sub-call per entry. That is what
// this file implements, and it is opt-in because the timing difference
// is observable: with it, an event may arrive after setValue returned.

// dispatchBufferSize is how many events may queue per remote before
// producers block. Deep enough that a burst of paramset writes never
// blocks, shallow enough that a dead-slow receiver cannot make the
// simulator hoard memory.
const dispatchBufferSize = 256

// dispatchLinger is how long the dispatcher waits for more events
// before sending what it has. It trades a barely perceptible delay for
// the batching a CCU does.
const dispatchLinger = 5 * time.Millisecond

// pendingEvent is one queued event callback.
type pendingEvent struct {
	interfaceID string
	address     string
	valueKey    string
	value       any
}

// dispatcher owns the delivery goroutine for a single remote.
type dispatcher struct {
	client remoteCaller
	queue  chan pendingEvent
	done   chan struct{}
	once   sync.Once
}

// newDispatcher starts the delivery goroutine for client. onGone is
// invoked when the remote turns out to be unreachable, so the owner can
// deregister it.
func newDispatcher(client remoteCaller, onGone func()) *dispatcher {
	d := &dispatcher{
		client: client,
		queue:  make(chan pendingEvent, dispatchBufferSize),
		done:   make(chan struct{}),
	}
	go d.run(onGone)
	return d
}

// enqueue hands an event to the dispatcher. It never blocks
// indefinitely: if the queue is full the event is dropped, which is
// what a CCU's bounded queue does under a receiver that cannot keep up.
func (d *dispatcher) enqueue(ev pendingEvent) {
	select {
	case d.queue <- ev:
	case <-d.done:
	default:
	}
}

// stop shuts the dispatcher down. Safe to call more than once.
func (d *dispatcher) stop() {
	d.once.Do(func() { close(d.done) })
}

// run drains the queue and delivers batches until stopped.
func (d *dispatcher) run(onGone func()) {
	for {
		select {
		case <-d.done:
			return
		case first := <-d.queue:
			batch := d.collect(first)
			if !d.deliver(batch) && onGone != nil {
				onGone()
				return
			}
		}
	}
}

// collect gathers everything that arrives within the linger window,
// starting from first.
func (d *dispatcher) collect(first pendingEvent) []pendingEvent {
	batch := []pendingEvent{first}
	timer := time.NewTimer(dispatchLinger)
	defer timer.Stop()
	for {
		select {
		case <-d.done:
			return batch
		case ev := <-d.queue:
			batch = append(batch, ev)
			if len(batch) >= dispatchBufferSize {
				return batch
			}
		case <-timer.C:
			return batch
		}
	}
}

// deliver sends a batch and reports whether the remote is still alive.
// A single event goes out as a plain `event` call, matching what a CCU
// sends when only one is pending; anything more is wrapped in
// `system.multicall`.
func (d *dispatcher) deliver(batch []pendingEvent) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if len(batch) == 1 {
		_, err := d.client.Call(ctx, "event", eventParams(batch[0]))
		return !xmlrpc.IsTransport(err)
	}

	calls := make(xmlrpc.ArrayValue, 0, len(batch))
	for _, ev := range batch {
		calls = append(calls, xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("event")},
			{Name: "params", Value: xmlrpc.ArrayValue(eventParams(ev))},
		}})
	}
	_, err := d.client.Call(ctx, "system.multicall", []xmlrpc.Value{calls})
	return !xmlrpc.IsTransport(err)
}

// eventParams renders one event as its XML-RPC argument list.
func eventParams(ev pendingEvent) []xmlrpc.Value {
	return []xmlrpc.Value{
		xmlrpc.StringValue(ev.interfaceID),
		xmlrpc.StringValue(ev.address),
		xmlrpc.StringValue(ev.valueKey),
		xmlrpc.FromAny(ev.value),
	}
}

// EnableBatchEvents switches event delivery to the asynchronous,
// batched dispatcher. Call before any client registers.
func (r *RPCFunctions) EnableBatchEvents() {
	r.mu.Lock()
	r.batchEvents = true
	r.mu.Unlock()
}

// dispatcherFor returns the dispatcher of a registered remote, starting
// one on first use. The caller must hold the lock.
func (r *RPCFunctions) dispatcherFor(interfaceID string, client remoteCaller) *dispatcher {
	if d, ok := r.dispatchers[interfaceID]; ok {
		return d
	}
	d := newDispatcher(client, func() {
		r.mu.Lock()
		delete(r.remotes, interfaceID)
		if existing, ok := r.dispatchers[interfaceID]; ok {
			delete(r.dispatchers, interfaceID)
			existing.stop()
		}
		r.mu.Unlock()
	})
	r.dispatchers[interfaceID] = d
	return d
}

// stopDispatchers shuts every delivery goroutine down.
func (r *RPCFunctions) stopDispatchers() {
	r.mu.Lock()
	dispatchers := r.dispatchers
	r.dispatchers = make(map[string]*dispatcher)
	r.mu.Unlock()
	for _, d := range dispatchers {
		d.stop()
	}
}
