// Package logging writes the process log to a rotating file so a packaged GUI
// build can be diagnosed. The desktop build has no console: stderr is discarded
// and every log line used to be lost, which left bug reports such as "warp-cli
// not found" or "切换失败" without any context.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxFileSize is the size at which the current file is rotated. A debug log
	// is not worth more than a few megabytes per day.
	maxFileSize = 2 << 20 // 2 MiB
	// filePrefix and filePattern define the file names:
	// bknetwork-20261027.log, and bknetwork-20261027.log.1 after a rotation.
	filePrefix = "bknetwork-"
	timeLayout = "20060102"
)

var (
	setupMu   sync.Mutex
	writer    *rotatingWriter
	logPath   string
	setupDone bool
)

// Dir returns the directory the log lives in:
// %LOCALAPPDATA%\BKNetwork\logs on Windows, the user cache directory
// elsewhere.
func Dir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("BKNETWORK_LOG_DIR")); dir != "" {
		return dir, nil
	}
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return filepath.Join(local, "BKNetwork", "logs"), nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "BKNetwork", "logs"), nil
}

// Setup redirects the standard logger to a file in the log directory while
// keeping stderr, so a console run still shows everything. It is safe to call
// more than once; only the first call configures the logger.
func Setup() (string, error) {
	setupMu.Lock()
	defer setupMu.Unlock()
	if setupDone {
		return logPath, nil
	}

	dir, err := Dir()
	if err != nil {
		return "", err
	}
	w, err := newRotatingWriter(dir, maxFileSize)
	if err != nil {
		return "", err
	}

	log.SetOutput(io.MultiWriter(os.Stderr, w))
	// Microsecond timestamps: several requests can log inside the same second
	// and the order matters when reconstructing a failure.
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("")

	writer = w
	logPath = w.currentPath()
	setupDone = true
	return logPath, nil
}

// CurrentPath returns the file the log is currently written to, or "" when
// Setup has not run.
func CurrentPath() string {
	setupMu.Lock()
	defer setupMu.Unlock()
	if writer == nil {
		return ""
	}
	return writer.currentPath()
}

// Audit writes one line per user visible action (switch, DNS change, settings
// change, WARP action) so a report can be traced back to what the app did.
func Audit(action string, fields map[string]string) {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("AUDIT action=")
	b.WriteString(action)
	for _, key := range keys {
		b.WriteString(" ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(quoteIfNeeded(fields[key]))
	}
	log.Print(b.String())
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\"") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

// Recent returns at most limit log lines, newest last. It reads the newest
// files only, so a report stays small even after months of logs.
func Recent(limit int) []string {
	dir, err := Dir()
	if err != nil || limit <= 0 {
		return nil
	}
	files, err := logFiles(dir)
	if err != nil || len(files) == 0 {
		return nil
	}

	// Files come newest first; collect until the limit is covered, then
	// reassemble oldest first so the caller sees a chronological tail.
	collected := make([][]string, 0, len(files))
	total := 0
	for _, file := range files {
		part := tailLines(file, limit)
		if len(part) == 0 {
			continue
		}
		collected = append(collected, part)
		total += len(part)
		if total >= limit {
			break
		}
	}

	lines := make([]string, 0, total)
	for i := len(collected) - 1; i >= 0; i-- {
		lines = append(lines, collected[i]...)
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

// logFiles lists the log files in dir, newest first. A rotated file
// (bknetwork-20261027.log.1) belongs to the same day and sorts right after it.
func logFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), filePrefix) {
			continue
		}
		names = append(names, entry.Name())
	}
	// Newest first: later day first, and for one day the plain file before its
	// rotation ("bknetwork-20261027.log" is newer than "...log.1").
	sort.Slice(names, func(i, j int) bool {
		dayI, rotationI := splitLogName(names[i])
		dayJ, rotationJ := splitLogName(names[j])
		if dayI != dayJ {
			return dayI > dayJ
		}
		return rotationI < rotationJ
	})
	files := make([]string, 0, len(names))
	for _, name := range names {
		files = append(files, filepath.Join(dir, name))
	}
	return files, nil
}

// splitLogName splits "bknetwork-20261027.log.2" into its day and rotation
// counter (0 for the current file, 1 for the first rotation).
func splitLogName(name string) (day string, rotation int) {
	rest := strings.TrimPrefix(name, filePrefix)
	day, suffix, found := strings.Cut(rest, ".log")
	if !found {
		return rest, 0
	}
	rotation = 0
	if trimmed := strings.TrimPrefix(suffix, "."); trimmed != "" {
		if value, err := strconv.Atoi(trimmed); err == nil {
			rotation = value
		}
	}
	return day, rotation
}

// tailLines reads the last limit lines of a file without loading all of it.
func tailLines(path string, limit int) []string {
	const maxRead = 256 << 10

	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	offset := int64(0)
	if size > maxRead {
		offset = size - maxRead
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRead))
	if err != nil {
		return nil
	}

	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if offset > 0 && len(lines) > 0 {
		// The first line is probably cut in half.
		lines = lines[1:]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

// rotatingWriter appends to one file per day and renames it when it grows past
// maxSize, keeping a single rotated file per day.
type rotatingWriter struct {
	mu      sync.Mutex
	dir     string
	maxSize int64

	file *os.File
	day  string
	size int64
}

func newRotatingWriter(dir string, maxSize int64) (*rotatingWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &rotatingWriter{dir: dir, maxSize: maxSize}
	if err := w.openLocked(time.Now()); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	if w.file == nil || w.day != now.Format(timeLayout) {
		if err := w.openLocked(now); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the current file. The writer is not usable afterwards.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingWriter) currentPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pathLocked(time.Now())
}

func (w *rotatingWriter) pathLocked(now time.Time) string {
	day := now.Format(timeLayout)
	if w.day != "" {
		day = w.day
	}
	return filepath.Join(w.dir, filePrefix+day+".log")
}

func (w *rotatingWriter) openLocked(now time.Time) error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	day := now.Format(timeLayout)
	path := filepath.Join(w.dir, filePrefix+day+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.day = day
	w.size = info.Size()
	return nil
}

// rotateLocked moves the current file to <name>.log.1 (replacing the previous
// rotation) and starts a new one, so a single day cannot fill the disk.
func (w *rotatingWriter) rotateLocked() error {
	path := filepath.Join(w.dir, filePrefix+w.day+".log")
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if err := os.Rename(path, path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.openLocked(time.Now())
}
