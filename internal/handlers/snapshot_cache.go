package handlers

import (
	"sync"
	"time"
)

const (
	// snapshotCacheTTL absorbs the status polling that follows an action (the UI
	// refreshes every 2s for ~15s) without collecting the network state - ten
	// external processes, dominated by a ~2.5s PowerShell call - for every
	// request. Mutations call invalidateNetworkSnapshot, so the TTL only ever
	// delays data that nothing has changed.
	snapshotCacheTTL = 5 * time.Second
	// snapshotErrorTTL is shorter: a failed collection is worth retrying sooner
	// than a successful one is worth repeating.
	snapshotErrorTTL = 500 * time.Millisecond
)

// snapshotProvider collects a fresh network snapshot.
type snapshotProvider func() (networkSnapshot, error)

// snapshotCall is one in-flight collection that concurrent callers wait on.
type snapshotCall struct {
	done chan struct{}
	snap networkSnapshot
	err  error
}

// snapshotCache caches the collected snapshot for a short time and collapses
// concurrent collections into a single one.
type snapshotCache struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time

	at       time.Time
	valid    bool
	snap     networkSnapshot
	err      error
	inflight *snapshotCall
	gen      uint64
}

func newSnapshotCache(ttl time.Duration) *snapshotCache {
	return &snapshotCache{ttl: ttl, now: time.Now}
}

// get returns a cached snapshot while it is fresh, joins an in-flight collection
// when one is running, and otherwise collects a new one.
func (c *snapshotCache) get(collect snapshotProvider) (networkSnapshot, error) {
	c.mu.Lock()
	if c.valid && c.fresh() {
		snap, err := c.snap, c.err
		c.mu.Unlock()
		return snap, err
	}
	if call := c.inflight; call != nil {
		c.mu.Unlock()
		<-call.done
		return call.snap, call.err
	}
	call := &snapshotCall{done: make(chan struct{})}
	c.inflight = call
	gen := c.gen
	c.mu.Unlock()

	snap, err := collect()

	c.mu.Lock()
	// A mutation invalidated the cache while this collection was in flight:
	// the result is already stale, so it must not be re-cached as valid.
	if c.gen == gen {
		c.snap, c.err, c.at, c.valid = snap, err, c.now(), true
	}
	c.inflight = nil
	c.mu.Unlock()

	// Publish before waking the waiters; the channel close orders the writes.
	call.snap, call.err = snap, err
	close(call.done)

	return snap, err
}

func (c *snapshotCache) fresh() bool {
	ttl := c.ttl
	if c.err != nil && snapshotErrorTTL < ttl {
		ttl = snapshotErrorTTL
	}
	return c.now().Sub(c.at) < ttl
}

// invalidate drops the cached snapshot so the next call collects fresh data.
// It is used after a mutation instead of waiting for the TTL to expire.
func (c *snapshotCache) invalidate() {
	c.mu.Lock()
	c.gen++
	c.valid = false
	c.at = time.Time{}
	c.mu.Unlock()
}

var networkSnapshots = newSnapshotCache(snapshotCacheTTL)

// getNetworkSnapshot returns the shared network snapshot, reusing a recent
// collection when possible.
func getNetworkSnapshot() (networkSnapshot, error) {
	return networkSnapshots.get(collectNetworkSnapshot)
}

// invalidateNetworkSnapshot forces the next getNetworkSnapshot to collect again.
func invalidateNetworkSnapshot() {
	networkSnapshots.invalidate()
}
