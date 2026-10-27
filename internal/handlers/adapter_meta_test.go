package handlers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdapterMetadataCacheReusesFreshEntry(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	var calls int32
	cache := newAdapterMetadataCache(time.Minute, func(context.Context) ([]adapterMeta, error) {
		atomic.AddInt32(&calls, 1)
		return []adapterMeta{{Name: "WiFi", MacAddress: "aa:bb:cc:dd:ee:ff"}}, nil
	})
	cache.now = clock.now

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		items, err := cache.get(ctx)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(items) != 1 || items[0].Name != "WiFi" {
			t.Fatalf("items = %v, want the WiFi adapter", items)
		}
	}

	// Still inside the TTL: no second query.
	clock.advance(30 * time.Second)
	if _, err := cache.get(ctx); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fetch called %d times, want 1 (the entry is still fresh)", got)
	}
}

func TestAdapterMetadataCacheServesStaleWhileRefreshing(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	release := make(chan struct{})
	var calls int32
	cache := newAdapterMetadataCache(time.Minute, func(context.Context) ([]adapterMeta, error) {
		if atomic.AddInt32(&calls, 1) > 1 {
			<-release
			return []adapterMeta{{Name: "WiFi", MacAddress: "new"}}, nil
		}
		return []adapterMeta{{Name: "WiFi", MacAddress: "old"}}, nil
	})
	cache.now = clock.now

	ctx := context.Background()
	if _, err := cache.get(ctx); err != nil {
		t.Fatalf("cold get: %v", err)
	}

	clock.advance(2 * time.Minute)

	start := time.Now()
	items, err := cache.get(ctx)
	if err != nil {
		t.Fatalf("stale get: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("stale get blocked for %s; it must return the cached value immediately", elapsed)
	}
	if items[0].MacAddress != "old" {
		t.Fatalf("stale get returned %q, want the cached value", items[0].MacAddress)
	}

	// A second call must not start another refresh while one is running.
	if _, err := cache.get(ctx); err != nil {
		t.Fatalf("get during refresh: %v", err)
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 2 })
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("fetch called %d times, want 2 (one cold, one refresh)", got)
	}

	close(release)
	waitFor(t, func() bool {
		items, err := cache.get(ctx)
		return err == nil && len(items) == 1 && items[0].MacAddress == "new"
	})
}

func TestAdapterMetadataCacheCollapsesConcurrentColdCalls(t *testing.T) {
	var calls int32
	cache := newAdapterMetadataCache(time.Minute, func(context.Context) ([]adapterMeta, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(30 * time.Millisecond)
		return []adapterMeta{{Name: "WiFi"}}, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.get(context.Background()); err != nil {
				t.Errorf("get: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fetch called %d times, want 1 (concurrent calls must collapse)", got)
	}
}

func TestAdapterMetadataCacheKeepsLastGoodListOnFailure(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	var calls int32
	cache := newAdapterMetadataCache(time.Minute, func(context.Context) ([]adapterMeta, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return []adapterMeta{{Name: "WiFi"}}, nil
		}
		return nil, errors.New("powershell exploded")
	})
	cache.now = clock.now

	ctx := context.Background()
	if _, err := cache.get(ctx); err != nil {
		t.Fatalf("cold get: %v", err)
	}

	clock.advance(2 * time.Minute)
	items, err := cache.get(ctx)
	if err != nil {
		t.Fatalf("stale get returned an error instead of the last good list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %v, want the cached adapter", items)
	}

	// The background refresh failed (it retries once); wait for it to settle,
	// then make sure a call shortly after does not start another one.
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) >= 3 })
	settled := atomic.LoadInt32(&calls)

	clock.advance(2 * time.Second)
	if _, err := cache.get(ctx); err != nil {
		t.Fatalf("get: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != settled {
		t.Fatalf("fetch called %d times after a failure, want %d (backoff)", got, settled)
	}
}

func TestMergeAdapterMetadataAddsUnknownAdapters(t *testing.T) {
	metas := []adapterMeta{{Name: "WiFi", MacAddress: "aa"}}
	ipv4 := newNetshInterfaceState()
	ipv4.Connected["WiFi"] = true
	ipv4.Known["WiFi"] = true
	ipv4.Index["5"] = "WiFi"
	ipv4.Index["9"] = "USB Ethernet"

	merged := mergeAdapterMetadata(metas, ipv4)
	if len(merged) != 2 {
		t.Fatalf("merged = %v, want the cached adapter plus the new one", merged)
	}
	names := []string{merged[0].Name, merged[1].Name}
	if names[0] != "USB Ethernet" && names[1] != "USB Ethernet" {
		t.Fatalf("merged names = %v, want USB Ethernet to be added", names)
	}
	for _, item := range merged {
		if item.Name == "5" || item.Name == "9" {
			t.Fatalf("merged = %v: interface indexes leaked into adapter names", merged)
		}
	}
	for _, item := range merged {
		if item.Name == "WiFi" && item.MacAddress != "aa" {
			t.Fatalf("cached metadata was lost: %+v", item)
		}
	}
}

func TestDeriveAdapterStatus(t *testing.T) {
	ipv4 := newNetshInterfaceState()
	ipv4.Connected["WiFi"] = true
	ipv4.Known["WiFi"] = true

	ipv6 := newNetshInterfaceState()
	ipv6.Connected["WiFi"] = true
	ipv6.Known["WiFi"] = true
	ipv6.Connected["Ethernet"] = false
	ipv6.Known["Ethernet"] = true

	unknown := newNetshInterfaceState()
	unknown.Connected["Tailscale"] = false
	unknown.Known["Tailscale"] = false

	tests := []struct {
		name  string
		meta  adapterMeta
		want  string
		state []netshInterfaceState
	}{
		{name: "up from netsh", meta: adapterMeta{Name: "WiFi", Status: "Disconnected"}, want: "Up"},
		{name: "down from netsh", meta: adapterMeta{Name: "Ethernet", Status: "Up"}, want: "Disconnected"},
		{name: "unknown language falls back", meta: adapterMeta{Name: "Tailscale", Status: "Up"}, want: "Up", state: []netshInterfaceState{unknown}},
		{name: "adapter missing from netsh", meta: adapterMeta{Name: "Bluetooth", Status: "Disabled"}, want: "Disabled"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			states := tc.state
			if states == nil {
				states = []netshInterfaceState{ipv4, ipv6}
			}
			if got := deriveAdapterStatus(tc.meta, states...); got != tc.want {
				t.Errorf("deriveAdapterStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeUniqueStrings(t *testing.T) {
	got := mergeUniqueStrings([]string{"1.1.1.1", "8.8.8.8"}, []string{"8.8.8.8", "2606:4700::1111", "  "})
	want := []string{"1.1.1.1", "8.8.8.8", "2606:4700::1111"}
	if len(got) != len(want) {
		t.Fatalf("mergeUniqueStrings() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mergeUniqueStrings() = %v, want %v", got, want)
		}
	}

	if got := mergeUniqueStrings(nil, nil); len(got) != 0 {
		t.Fatalf("mergeUniqueStrings(nil, nil) = %v, want empty", got)
	}
}

// waitFor polls condition until it holds or a short deadline expires.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met within the deadline")
}
