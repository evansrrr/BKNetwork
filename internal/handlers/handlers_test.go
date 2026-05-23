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
