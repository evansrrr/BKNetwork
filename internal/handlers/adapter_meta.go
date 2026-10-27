package handlers

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// adapterMeta is the adapter information that does not change while the app is
// running: name, MAC address and description. It comes from Get-NetAdapter,
// which is by far the most expensive call of a snapshot (a cold PowerShell
// start, ~2.5s on the development machine), so it is cached and refreshed in
// the background instead of being collected on every poll.
type adapterMeta struct {
	Name        string
	MacAddress  string
	Description string
	// Status is the last status PowerShell reported. It is only used when the
	// netsh state word could not be translated.
	Status string
}

const (
	// adapterMetadataTTL bounds how long cached metadata is reused. The list
	// only changes when a device is added or removed.
	adapterMetadataTTL = 60 * time.Second
	// adapterMetadataTimeout bounds a single refresh.
	adapterMetadataTimeout = timeoutShort
	// adapterMetadataRetryDelay is the pause between the two fetch attempts of
	// a cold start; the first PowerShell call after boot sometimes fails.
	adapterMetadataRetryDelay = 150 * time.Millisecond
	// adapterMetadataFailureRetry is how long a failed background refresh waits
	// before trying again, so a broken PowerShell does not cost a process per
	// status request.
	adapterMetadataFailureRetry = 10 * time.Second
)

type adapterMetadataCall struct {
	done  chan struct{}
	items []adapterMeta
	err   error
}

// adapterMetadataCache serves adapter metadata from memory and refreshes it in
// the background (stale-while-revalidate), so no request ever waits for the
// PowerShell start except the very first one.
type adapterMetadataCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	fetch func(context.Context) ([]adapterMeta, error)

	items     []adapterMeta
	fetchedAt time.Time
	lastErr   error

	// inflight is set while a cold fetch runs; concurrent callers wait on it
	// instead of starting another one.
	inflight *adapterMetadataCall
	// refreshing guards the background refresh.
	refreshing bool
}

func newAdapterMetadataCache(ttl time.Duration, fetch func(context.Context) ([]adapterMeta, error)) *adapterMetadataCache {
	return &adapterMetadataCache{ttl: ttl, now: time.Now, fetch: fetch}
}

// get returns the cached metadata, triggering a background refresh when the
// entry is stale. It blocks only when there is nothing cached yet.
func (c *adapterMetadataCache) get(ctx context.Context) ([]adapterMeta, error) {
	c.mu.Lock()
	if len(c.items) > 0 {
		items := c.items
		if !c.freshLocked() {
			c.refreshInBackgroundLocked()
		}
		c.mu.Unlock()
		return items, nil
	}
	if call := c.inflight; call != nil {
		c.mu.Unlock()
		<-call.done
		return call.items, call.err
	}
	call := &adapterMetadataCall{done: make(chan struct{})}
	c.inflight = call
	c.mu.Unlock()

	fetchCtx, cancel := context.WithTimeout(ctx, adapterMetadataTimeout)
	defer cancel()
	items, err := c.fetchWithRetry(fetchCtx)

	c.mu.Lock()
	if err == nil && len(items) > 0 {
		c.items, c.fetchedAt, c.lastErr = items, c.now(), nil
	} else {
		c.lastErr = err
	}
	c.inflight = nil
	c.mu.Unlock()

	call.items, call.err = items, err
	close(call.done)
	return items, err
}

func (c *adapterMetadataCache) freshLocked() bool {
	return !c.fetchedAt.IsZero() && c.now().Sub(c.fetchedAt) < c.ttl
}

func (c *adapterMetadataCache) refreshInBackgroundLocked() {
	if c.refreshing {
		return
	}
	c.refreshing = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), adapterMetadataTimeout)
		defer cancel()

		items, err := c.fetchWithRetry(ctx)

		c.mu.Lock()
		defer c.mu.Unlock()
		c.refreshing = false
		if err == nil && len(items) > 0 {
			c.items, c.fetchedAt, c.lastErr = items, c.now(), nil
			return
		}
		// Keep serving the last good list and retry later instead of on every
		// request.
		c.lastErr = err
		if err != nil {
			log.Printf("snapshot: adapter metadata refresh failed: %v", err)
		}
		c.fetchedAt = c.now().Add(-c.ttl + adapterMetadataFailureRetry)
	}()
}

func (c *adapterMetadataCache) fetchWithRetry(ctx context.Context) ([]adapterMeta, error) {
	items, err := c.fetch(ctx)
	if err == nil && len(items) > 0 {
		return items, nil
	}
	if ctx.Err() != nil {
		return items, err
	}
	select {
	case <-ctx.Done():
		return items, err
	case <-time.After(adapterMetadataRetryDelay):
	}
	retried, retryErr := c.fetch(ctx)
	if retryErr == nil && len(retried) > 0 {
		return retried, nil
	}
	if err == nil {
		err = retryErr
	}
	return retried, err
}

// fetchAdapterMetadata runs the PowerShell query. It returns the adapters in
// PowerShell order.
func fetchAdapterMetadata(ctx context.Context) ([]adapterMeta, error) {
	const command = "Get-NetAdapter | Select-Object Name, Status, MacAddress, InterfaceDescription | ConvertTo-Json -Compress"
	raw, err := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	basics, decodeErr := decodeJSONList[adapterBasic](raw)
	if decodeErr != nil {
		return nil, decodeErr
	}
	if len(basics) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	items := make([]adapterMeta, 0, len(basics))
	for _, basic := range basics {
		name := strings.TrimSpace(basic.Name)
		if name == "" {
			continue
		}
		items = append(items, adapterMeta{
			Name:        name,
			MacAddress:  basic.MacAddress,
			Description: basic.InterfaceDescription,
			Status:      basic.Status,
		})
	}
	return items, nil
}

var adapterMetadata = newAdapterMetadataCache(adapterMetadataTTL, fetchAdapterMetadata)

// adapterMetadataList is the entry point used by the snapshot collector.
func adapterMetadataList(ctx context.Context) ([]adapterMeta, error) {
	return adapterMetadata.get(ctx)
}

// mergeAdapterMetadata adds adapters that netsh reports but the cached metadata
// does not know yet (a device plugged in since the last refresh), so a new
// adapter appears immediately with an empty MAC and description.
func mergeAdapterMetadata(items []adapterMeta, states ...netshInterfaceState) []adapterMeta {
	seen := make(map[string]struct{}, len(items))
	merged := make([]adapterMeta, 0, len(items))
	for _, item := range items {
		seen[strings.ToLower(item.Name)] = struct{}{}
		merged = append(merged, item)
	}
	for _, state := range states {
		for _, name := range state.Index {
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, adapterMeta{Name: name})
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return strings.ToLower(merged[i].Name) < strings.ToLower(merged[j].Name)
	})
	return merged
}

// deriveAdapterStatus reports the link status from the netsh state columns,
// which are collected on every snapshot anyway. It falls back to the cached
// PowerShell status when the state word of the system language is not in
// netshConnectedWords / netshDisconnectedWords.
func deriveAdapterStatus(meta adapterMeta, states ...netshInterfaceState) string {
	known := false
	up := false
	for _, state := range states {
		if !state.Known[meta.Name] {
			continue
		}
		known = true
		if state.Connected[meta.Name] {
			up = true
		}
	}
	if !known {
		return meta.Status
	}
	if up {
		return "Up"
	}
	return "Disconnected"
}
