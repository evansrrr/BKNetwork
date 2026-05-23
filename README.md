BKNetwork
=========

Minimal scaffold for BKNetwork — Windows background service with local web UI.

Quick run (console):

```powershell
cd bknetwork
go run ./cmd/bknetwork
```

Build:

```powershell
cd bknetwork
go build -o bknetwork.exe ./cmd/bknetwork
```

Install as service (requires Administrator):

```powershell
.
# as Administrator
bknetwork.exe install
bknetwork.exe start
```

Notes:
- The scaffold exposes REST endpoints under `/api/v1/` and a websocket at `/ws`.
- Commands that modify network or control `warp-cli` require Administrator privileges.
- This is a starting point — network commands are placeholders and must be hardened.
