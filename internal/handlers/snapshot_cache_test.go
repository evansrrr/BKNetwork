package handlers

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock lets the tests move time forward deterministically.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

func TestSnapshotCacheReusesFreshResult(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cache := newSnapshotCache(time.Second)
	cache.now = clock.now

	var calls int32
	collect := func() (networkSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return networkSnapshot{CollectedAt: "snap"}, nil
	}

	for i := 0; i < 3; i++ {
		snap, err := cache.get(collect)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if snap.CollectedAt != "snap" {
			t.Fatalf("unexpected snapshot: %+v", snap)
		}
		clock.advance(200 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("collector called %d times within the TTL, want 1", got)
	}

	clock.advance(2 * time.Second)
	if _, err := cache.get(collect); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("collector called %d times after the TTL, want 2", got)
	}
}

func TestSnapshotCacheInvalidateForcesCollect(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cache := newSnapshotCache(time.Minute)
	cache.now = clock.now

	var calls int32
	collect := func() (networkSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return networkSnapshot{}, nil
	}

	if _, err := cache.get(collect); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cache.invalidate()
	if _, err := cache.get(collect); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("collector called %d times, want 2", got)
	}
}

func TestSnapshotCacheRetriesErrorsSooner(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	cache := newSnapshotCache(time.Minute)
	cache.now = clock.now

	var calls int32
	collect := func() (networkSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return networkSnapshot{}, errors.New("adapter list unavailable")
	}

	if _, err := cache.get(collect); err == nil {
		t.Fatal("expected the first collection to fail")
	}
	// Within the error TTL the error is reused.
	if _, err := cache.get(collect); err == nil {
		t.Fatal("expected the cached error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("collector called %d times, want 1 inside the error TTL", got)
	}

	clock.advance(snapshotErrorTTL + time.Millisecond)
	if _, err := cache.get(collect); err == nil {
		t.Fatal("expected the retry to fail as well")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("collector called %d times, want a retry after the error TTL", got)
	}
}

// TestSnapshotCacheCollapsesConcurrentCalls mirrors a page load where the
// WebSocket handshake and the status poll arrive at the same time.
func TestSnapshotCacheCollapsesConcurrentCalls(t *testing.T) {
	cache := newSnapshotCache(time.Minute)

	var calls int32
	release := make(chan struct{})
	collect := func() (networkSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return networkSnapshot{CollectedAt: "shared"}, nil
	}

	const workers = 8
	var wg sync.WaitGroup
	results := make([]string, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			snap, err := cache.get(collect)
			if err != nil {
				t.Errorf("worker %d: unexpected error: %v", index, err)
				return
			}
			results[index] = snap.CollectedAt
		}(i)
	}

	// Give the workers a moment to pile up on the in-flight collection.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("collector called %d times for %d concurrent readers, want 1", got, workers)
	}
	for i, result := range results {
		if result != "shared" {
			t.Fatalf("worker %d received %q, want the shared snapshot", i, result)
		}
	}
}
