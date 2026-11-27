package handlers

import (
	"errors"
	"net/http"
	"testing"
)

func TestNormalizeAdapterName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "plain", input: "WiFi", want: "WiFi"},
		{name: "trimmed", input: "  Kid's Wi-Fi  ", want: "Kid's Wi-Fi"},
		{name: "empty", input: "   ", wantErr: errAdapterNotFound},
		{name: "star", input: "*", wantErr: errAdapterWildcard},
		{name: "question", input: "Ethernet ?", wantErr: errAdapterWildcard},
		{name: "bracket", input: "Ethernet [4]", wantErr: errAdapterWildcard},
		{name: "loopback", input: "Loopback Pseudo-Interface 1", wantErr: errAdapterNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAdapterName(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMatchAdapterTarget(t *testing.T) {
	metas := []adapterMeta{
		{Name: "WiFi", Description: "MediaTek Wi-Fi 6 MT7921 Wireless LAN Card"},
		{Name: "Ethernet 4", Description: "Netease UU TAP-Win32 Adapter V9.21"},
		{Name: "Tailscale", Description: "Tailscale Tunnel"},
	}

	t.Run("physical adapter", func(t *testing.T) {
		if err := matchAdapterTarget(metas, "wifi", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("virtual adapter rejected", func(t *testing.T) {
		if err := matchAdapterTarget(metas, "Ethernet 4", false); !errors.Is(err, errAdapterVirtual) {
			t.Fatalf("err = %v, want errAdapterVirtual", err)
		}
	})
	t.Run("virtual adapter allowed for restore", func(t *testing.T) {
		if err := matchAdapterTarget(metas, "Ethernet 4", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("unknown adapter", func(t *testing.T) {
		if err := matchAdapterTarget(metas, "Nope", false); !errors.Is(err, errAdapterNotFound) {
			t.Fatalf("err = %v, want errAdapterNotFound", err)
		}
	})
}

func TestAdapterValidationStatus(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{errAdapterWildcard, http.StatusBadRequest, "invalid-adapter-name"},
		{errAdapterNotFound, http.StatusBadRequest, "adapter-not-found"},
		{errAdapterVirtual, http.StatusConflict, "virtual-adapter"},
		{errAdapterNotUp, http.StatusConflict, "adapter-not-connected"},
		{errors.New("other"), http.StatusInternalServerError, "adapter-check-failed"},
	}
	for _, tc := range cases {
		status, code := adapterValidationStatus(tc.err)
		if status != tc.wantStatus || code != tc.wantCode {
			t.Fatalf("%v -> (%d,%q), want (%d,%q)", tc.err, status, code, tc.wantStatus, tc.wantCode)
		}
	}
}
