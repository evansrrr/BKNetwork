package handlers

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	appsettings "bknetwork/internal/settings"
)

const (
	// versionCacheTTL is how long a successful release tag is reused. The check
	// used to hit GitHub on every page load, which is both slow on a restricted
	// network and rate limited (60 requests per hour without a token).
	versionCacheTTL = time.Hour
	// versionErrorTTL retries a failed check much sooner, but still not on every
	// page load.
	versionErrorTTL = 10 * time.Minute
	// versionTimeout bounds one release lookup. It is shorter than timeoutMedium
	// because the UI only shows a hint; a slow network should not hold it.
	versionTimeout = 6 * time.Second
	// versionRedirectTimeout bounds only the /releases/latest redirect hop, so a
	// slow redirect still leaves the API fallback a share of the overall budget.
	versionRedirectTimeout = 3 * time.Second
)

// versionTagCache caches the latest release tag and collapses concurrent
// lookups into one request.
type versionTagCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	errorTTL time.Duration
	now      func() time.Time
	fetch    func(context.Context) (string, error)

	tag   string
	err   error
	at    time.Time
	valid bool

	inflight chan struct{}
}

func newVersionTagCache(ttl, errorTTL time.Duration, fetch func(context.Context) (string, error)) *versionTagCache {
	return &versionTagCache{ttl: ttl, errorTTL: errorTTL, now: time.Now, fetch: fetch}
}

func (c *versionTagCache) get(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.valid {
		ttl := c.ttl
		if c.err != nil {
			ttl = c.errorTTL
		}
		if c.now().Sub(c.at) < ttl {
			tag, err := c.tag, c.err
			c.mu.Unlock()
			return tag, err
		}
	}
	if wait := c.inflight; wait != nil {
		c.mu.Unlock()
		<-wait
		c.mu.Lock()
		tag, err := c.tag, c.err
		c.mu.Unlock()
		return tag, err
	}
	done := make(chan struct{})
	c.inflight = done
	c.mu.Unlock()

	tag, err := c.fetch(ctx)

	c.mu.Lock()
	result, resultErr := tag, err
	switch {
	case err == nil:
		c.tag, c.err, c.at, c.valid = tag, nil, c.now(), true
	case !c.valid || c.err != nil:
		// Nothing usable yet: remember the failure, with the shorter TTL.
		c.tag, c.err, c.at, c.valid = "", err, c.now(), true
	default:
		// A previous success beats nothing: keep serving it and retry after the
		// error TTL instead of failing the UI on a flaky network.
		c.at = c.now().Add(-c.ttl + c.errorTTL)
		result, resultErr = c.tag, nil
	}
	c.inflight = nil
	c.mu.Unlock()
	close(done)

	return result, resultErr
}

// invalidate drops the cached tag, so the next call queries again.
func (c *versionTagCache) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.mu.Unlock()
}

var latestReleaseTag = newVersionTagCache(versionCacheTTL, versionErrorTTL, fetchLatestReleaseTag)

// onlineChecksDisabled reports whether the user turned the outbound checks off.
func onlineChecksDisabled() bool {
	cfg, err := appsettings.Load()
	if err != nil {
		return false
	}
	return cfg.DisableOnlineChecks
}

func LatestVersionHandler() http.HandlerFunc {
	return latestVersionHandler(onlineChecksDisabled, latestReleaseTag.get)
}

func latestVersionHandler(disabled func() bool, lookup func(context.Context) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		if disabled() {
			writeJSON(w, map[string]interface{}{"ok": true, "tag": "", "disabled": true}, http.StatusOK)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), versionTimeout)
		defer cancel()

		tag, err := lookup(ctx)
		if err != nil {
			log.Printf("latest version check failed: %v", err)
			writeJSON(w, map[string]interface{}{"ok": true, "tag": "", "error": err.Error()}, http.StatusOK)
			return
		}

		writeJSON(w, map[string]interface{}{"ok": true, "tag": tag}, http.StatusOK)
	}
}
