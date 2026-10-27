package handlers

import "testing"

func TestParseWarpConnected(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		want       bool
		wantStatus string
	}{
		{
			name:       "fully connected",
			raw:        "Status update: Connected\nNetwork: healthy\n",
			want:       true,
			wantStatus: "Connected",
		},
		{
			name:       "connecting",
			raw:        "Status: Connecting\n",
			want:       false,
			wantStatus: "Connecting",
		},
		{
			name:       "disabled",
			raw:        "Status: Disabled\n",
			want:       false,
			wantStatus: "Disabled",
		},
		{
			name:       "disconnected",
			raw:        "Status: Disconnected\n",
			want:       false,
			wantStatus: "Disconnected",
		},
		{
			name:       "connected but network unhealthy",
			raw:        "Status update: Connected\nNetwork: down\n",
			want:       false,
			wantStatus: "Connected",
		},
		{
			name:       "connected but network unstable",
			raw:        "Status update: Connected\nNetwork: unstable\n",
			want:       true,
			wantStatus: "Connected",
		},
		{
			name:       "empty input",
			raw:        "",
			want:       false,
			wantStatus: "",
		},
		{
			name:       "checking for update",
			raw:        "Status: Checking for update\n",
			want:       false,
			wantStatus: "Checking for update",
		},
		{
			// Regression: the old keyword check treated every value containing
			// "connect" as not connected, so plain "Status: Connected" (older
			// warp-cli builds, no network line) was judged offline forever.
			name:       "plain connected without network line",
			raw:        "Status: Connected\n",
			want:       true,
			wantStatus: "Connected",
		},
		{
			name:       "connected without network line (update form)",
			raw:        "Status update: Connected\nReason: Manual\n",
			want:       true,
			wantStatus: "Connected",
		},
		{
			name:       "CRLF line endings",
			raw:        "Status update: Connected\r\nNetwork: unstable\r\n",
			want:       true,
			wantStatus: "Connected",
		},
		{
			name:       "BOM prefix",
			raw:        "\ufeffStatus: Connected\n",
			want:       true,
			wantStatus: "Connected",
		},
		{
			// Localized output must degrade to "unknown" instead of guessing.
			name:       "localized connected",
			raw:        "状态: 已连接\n",
			want:       false,
			wantStatus: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotStatus := parseWarpConnected(tc.raw)
			if got != tc.want {
				t.Fatalf("parseWarpConnected() connected = %v, want %v", got, tc.want)
			}
			if gotStatus != tc.wantStatus {
				t.Fatalf("parseWarpConnected() status = %q, want %q", gotStatus, tc.wantStatus)
			}
		})
	}
}

func TestParseWarpStatusJSON(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		want       bool
		wantStatus string
		wantOK     bool
	}{
		{"lowercase key", `{"status": "Connected"}`, true, "Connected", true},
		{"capitalized key", `{"Status": "Connected"}`, true, "Connected", true},
		{"disconnected", `{"status": "Disconnected"}`, false, "Disconnected", true},
		{"connecting", `{"status": "Connecting"}`, false, "Connecting", true},
		{"boolean connected key", `{"connected": true}`, true, "Connected", true},
		{"boolean false", `{"connected": false}`, false, "Disconnected", true},
		{"no status field", `{"mode": "warp"}`, false, "", false},
		{"not json", "Status update: Connected", false, "", false},
		{"broken json", `{"status": "Connected"`, false, "", false},
		{"bom json", "\ufeff{\"status\": \"Connected\"}", true, "Connected", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, gotStatus, ok := parseWarpStatusJSON(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("parseWarpStatusJSON() ok = %v, want %v", ok, tc.wantOK)
			}
			if got != tc.want || gotStatus != tc.wantStatus {
				t.Fatalf("parseWarpStatusJSON() = (%v, %q), want (%v, %q)", got, gotStatus, tc.want, tc.wantStatus)
			}
		})
	}
}

func TestSelectHighestReleaseTag(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
		ok   bool
	}{
		{
			name: "prefer higher major version",
			tags: []string{"v0.9.9", "v1.0.0", "v0.9.8"},
			want: "v1.0.0",
			ok:   true,
		},
		{
			name: "stable beats prerelease on same core",
			tags: []string{"v1.0.0-beta.1", "v1.0.0"},
			want: "v1.0.0",
			ok:   true,
		},
		{
			name: "ignore non-semver tags",
			tags: []string{"latest", "release-2026", "v1.2.3"},
			want: "v1.2.3",
			ok:   true,
		},
		{
			name: "no valid tags",
			tags: []string{"latest", "release-2026"},
			want: "",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectHighestReleaseTag(tc.tags)
			if ok != tc.ok {
				t.Fatalf("selectHighestReleaseTag() ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("selectHighestReleaseTag() = %q, want %q", got, tc.want)
			}
		})
	}
}
