package buildinfo

// Version is the application version reported by the API, the UI and the log
// header. It mirrors the version in src-tauri/Cargo.toml, which is what Tauri
// uses for the bundle and what the release workflow checks the git tag against;
// a unit test keeps the two in sync.
//
// Release builds still override it with
// -X bknetwork/internal/buildinfo.Version=<version> so a binary built from a tag
// never has to be edited first.
var Version = "1.2.0"

// Repository is the upstream GitHub owner/name used by the in-app release check.
const Repository = "evansrrr/BKNetwork"
