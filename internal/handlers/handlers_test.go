package handlers

import "testing"

func TestParseWarpConnected(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "status update connected",
			raw:  "Status update: Connected\n",
			want: true,
		},
		{
			name: "status not connected",
			raw:  "Status: Not connected\n",
			want: false,
		},
		{
			name: "status line beats other text",
			raw:  "Status: Connected\nNotes: disconnected tunnel recheck later\n",
			want: true,
		},
		{
			name: "warp off",
			raw:  "Warp is off\n",
			want: false,
		},
		{
			name: "chinese connected",
			raw:  "状态：已连接\n",
			want: true,
		},
		{
			name: "chinese disconnected",
			raw:  "状态：未连接\n",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseWarpConnected(tc.raw); got != tc.want {
				t.Fatalf("parseWarpConnected() = %v, want %v", got, tc.want)
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
