package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestVersionTagCacheReusesFreshTag(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	var calls int32
	cache := newVersionTagCache(time.Hour, 10*time.Minute, func(context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "v1.5.0", nil
	})
	cache.now = clock.now

	for i := 0; i < 3; i++ {
		tag, err := cache.get(context.Background())
		if err != nil || tag != "v1.5.0" {
			t.Fatalf("get() = (%q, %v), want v1.5.0", tag, err)
		}
	}

	clock.advance(59 * time.Minute)
	if _, err := cache.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lookup called %d times, want 1 within the TTL", got)
	}

	clock.advance(2 * time.Minute)
	if _, err := cache.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("lookup called %d times, want 2 after the TTL", got)
	}
}

func TestVersionTagCacheRetriesFailuresSooner(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	var calls int32
	cache := newVersionTagCache(time.Hour, time.Minute, func(context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", errors.New("network unreachable")
	})
	cache.now = clock.now

	if _, err := cache.get(context.Background()); err == nil {
		t.Fatal("expected the first lookup to fail")
	}
	clock.advance(30 * time.Second)
	if _, err := cache.get(context.Background()); err == nil {
		t.Fatal("expected the cached failure to be returned")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lookup called %d times, want the failure to be cached", got)
	}

	clock.advance(45 * time.Second)
	_, _ = cache.get(context.Background())
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("lookup called %d times, want a retry after the shorter error TTL", got)
	}
}

func TestVersionTagCacheKeepsLastGoodTagWhenRefreshFails(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	var calls int32
	cache := newVersionTagCache(time.Minute, 10*time.Second, func(context.Context) (string, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return "v1.5.0", nil
		}
		return "", errors.New("timeout")
	})
	cache.now = clock.now

	if _, err := cache.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	// The refresh fails, but the tag from before the failure is still served so
	// a flaky network does not blank the update hint.
	clock.advance(2 * time.Minute)
	tag, err := cache.get(context.Background())
	if err != nil || tag != "v1.5.0" {
		t.Fatalf("get() = (%q, %v), want the cached tag", tag, err)
	}

	// The next call inside the retry window reuses it without another lookup.
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("lookup called %d times, want 2", got)
	}
	if _, err := cache.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("lookup called %d times, want the failure to back off", got)
	}
}

func TestVersionTagCacheCollapsesConcurrentLookups(t *testing.T) {
	var calls int32
	cache := newVersionTagCache(time.Hour, time.Minute, func(context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(30 * time.Millisecond)
		return "v1.5.0", nil
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
		t.Fatalf("lookup called %d times, want 1", got)
	}
}

func TestLatestVersionHandlerSkipsWhenDisabled(t *testing.T) {
	var called int32
	handler := latestVersionHandler(
		func() bool { return true },
		func(context.Context) (string, error) {
			atomic.AddInt32(&called, 1)
			return "v9.9.9", nil
		},
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/version/latest", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Fatalf("lookup ran %d times, want 0 when the checks are disabled", got)
	}

	var payload struct {
		OK       bool   `json:"ok"`
		Tag      string `json:"tag"`
		Disabled bool   `json:"disabled"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.OK || !payload.Disabled || payload.Tag != "" {
		t.Fatalf("payload = %+v, want an ok, disabled, empty-tag answer", payload)
	}
}

func TestLatestVersionHandlerReturnsTag(t *testing.T) {
	handler := latestVersionHandler(
		func() bool { return false },
		func(context.Context) (string, error) { return "v1.5.0", nil },
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/version/latest", nil))

	var payload struct {
		OK  bool   `json:"ok"`
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Tag != "v1.5.0" {
		t.Fatalf("payload = %+v, want the release tag", payload)
	}
}

func TestLatestVersionHandlerRejectsPost(t *testing.T) {
	handler := latestVersionHandler(
		func() bool { return false },
		func(context.Context) (string, error) { return "", nil },
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/version/latest", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
}
