package web

import "embed"

// Assets contains the complete browser UI so the Go sidecar remains
// self-contained when it is bundled inside the Tauri application.
//
//go:embed *.css *.html *.ico *.js *.svg
var Assets embed.FS
