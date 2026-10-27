package handlers

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"bknetwork/internal/buildinfo"
	"bknetwork/internal/logging"
	appsettings "bknetwork/internal/settings"
)

// diagnosticsLogLines is how much of the log a report carries. Enough to cover
// a failed action, small enough to paste into a chat or an issue.
const diagnosticsLogLines = 300

// DiagnosticsHandler returns one JSON document with everything a bug report
// needs: versions, paths, the tail of the log file and the current snapshot.
//
// The packaged build has no visible log, so this endpoint is the only way a
// user can hand over what actually happened.
func DiagnosticsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		logDir, dirErr := logging.Dir()
		response := map[string]interface{}{
			"generatedAt": time.Now().Format(time.RFC3339),
			"version":     buildinfo.Version,
			"os":          runtime.GOOS + "/" + runtime.GOARCH,
			"goVersion":   runtime.Version(),
			"executable":  executablePath(),
			"logDir":      logDir,
			"logPath":     logging.CurrentPath(),
			"logs":        logging.Recent(diagnosticsLogLines),
		}
		if dirErr != nil {
			response["logDirError"] = dirErr.Error()
		}

		if cfg, err := appsettings.Load(); err != nil {
			response["settingsError"] = err.Error()
		} else {
			response["settings"] = snapshotOf(cfg)
		}

		snap, snapErr := getNetworkSnapshot()
		if snapErr != nil {
			response["networkError"] = snapErr.Error()
		}
		response["network"] = snap

		writeJSON(w, response, http.StatusOK)
	}
}

func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}

// audit records a user visible action in the log file. The request id ties the
// line to the HTTP request that caused it, which is what makes a report from a
// packaged build actionable.
func audit(r *http.Request, action string, fields map[string]string) {
	if fields == nil {
		fields = map[string]string{}
	}
	if id := RequestID(r); id != "" {
		fields["requestId"] = id
	}
	logging.Audit(action, fields)
}
