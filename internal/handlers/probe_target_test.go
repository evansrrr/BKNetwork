package handlers

import (
	"context"
	"testing"
	"time"
)

func TestValidProbeTarget(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"cloudflare.com:443", true},
		{"[2001:db8::1]:443", true},
		{"192.168.1.1:53", true},
		{"  example.com:8080  ", true},
		{"", false},
		{"cloudflare.com", false},
		{":443", false},
		{"host:notaport", false},
		{"host:0", false},
		{"host:70000", false},
	}
	for _, tc := range tests {
		if got := validProbeTarget(tc.target); got != tc.want {
			t.Errorf("validProbeTarget(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}

func TestCachedTCPProbeReprobesOnTargetChange(t *testing.T) {
	tcpProbeLast = tcpProbeSnapshot{}
	tcpProbeAt = time.Time{}
	defer func() {
		tcpProbeLast = tcpProbeSnapshot{}
		tcpProbeAt = time.Time{}
	}()

	calls := 0
	prober := func(_ context.Context, target string) tcpProbeSnapshot {
		calls++
		return tcpProbeSnapshot{Target: target, OK: true}
	}

	first := cachedTCPProbe(context.Background(), "cloudflare.com:443", prober)
	if first.Target != "cloudflare.com:443" || calls != 1 {
		t.Fatalf("first = %+v calls = %d, want one probe of cloudflare.com:443", first, calls)
	}

	// A fresh call inside the TTL serves the cache.
	if again := cachedTCPProbe(context.Background(), "cloudflare.com:443", prober); again.Target != "cloudflare.com:443" || calls != 1 {
		t.Fatalf("cached = %+v calls = %d, want the cached result", again, calls)
	}

	// A different target must not be answered from the old host's cache.
	switched := cachedTCPProbe(context.Background(), "example.com:443", prober)
	if switched.Target != "example.com:443" || calls != 2 {
		t.Fatalf("switched = %+v calls = %d, want a re-probe for the new target", switched, calls)
	}
}
