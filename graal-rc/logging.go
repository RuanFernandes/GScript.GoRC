package main

import (
	"errors"
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
	defaultLogMaxBytes = int64(10 << 20)
	defaultLogMaxFiles = 14

	logFilePrefix = "app_"
	logFileSuffix = ".log"
)

// logFile is deliberately independent from the rotating application log. The
// Windows VEH writes to it directly from the faulting OS thread; keeping this
// descriptor stable means a size/day rotation cannot close it underneath a
// crash handler. It is a per-day crash sink, not a buffered Go writer.
var logFile *os.File

var (
	fileLoggerMu sync.Mutex
	fileLogger   *rotatingLogWriter
)

type loggerConfig struct {
	maxBytes int64
	maxFiles int
}

// rotatingLogWriter is the stdlib log.Writer used by the application. It owns
// one active file at a time and serializes writes, rotation, cleanup, and Sync.
// The mirror receives the already-redacted bytes, never the original log line.
type rotatingLogWriter struct {
	mu sync.Mutex

	dir      string
	maxBytes int64
	maxFiles int
	now      func() time.Time
	mirror   io.Writer

	file      *os.File
	path      string
	crashPath string
	date      string
	size      int64
	closed    bool
}

// redactingWriter keeps the stderr-only fallback safe as well. In particular,
// using io.MultiWriter directly here would mirror the unredacted input.
type redactingWriter struct {
	dst io.Writer
}

// initFileLogger redirects the stdlib log package to a per-user rotating log.
// In a -H windowsgui release there is no console, so a writable fallback under
// the OS temp directory is attempted before falling back to redacted stderr.
//
// Environment overrides:
//   - GRAAL_RC_LOG_DIR: explicit log directory (tried before defaults)
//   - GRAAL_RC_LOG_MAX_BYTES / GRAAL_RC_LOG_MAX_SIZE: size limit per segment
//   - GRAAL_RC_LOG_RETENTION / GRAAL_RC_LOG_MAX_FILES: retained log files
//   - GRAAL_RC_LOG_STDERR: mirror redacted logs to stderr when non-empty
func initFileLogger() {
	_ = CloseFileLogger()
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	dir, err := resolveLogDir()
	if err != nil {
		configureFallbackLogger(err)
		return
	}

	cfg := loadLoggerConfig()
	writer, err := newRotatingLogWriter(dir, cfg.maxBytes, cfg.maxFiles)
	if err != nil {
		configureFallbackLogger(err)
		return
	}
	if envEnabled("GRAAL_RC_LOG_STDERR") {
		writer.mirror = os.Stderr
	}

	crash, crashPath, crashErr := openCrashLog(dir, time.Now())
	if crashErr != nil {
		// The ordinary log remains useful even if the separate VEH sink cannot
		// be opened. The crash handler checks logFile for nil.
		crashPath = ""
	}
	writer.crashPath = crashPath

	fileLoggerMu.Lock()
	fileLogger = writer
	logFile = crash
	fileLoggerMu.Unlock()

	log.SetOutput(writer)
	if crashErr != nil {
		log.Printf("logger initialized -> %s (crash sink unavailable: %v)", writer.path, crashErr)
		return
	}

	// Include the crash sink in retention cleanup, while protecting both files
	// that are currently open. Cleanup is intentionally best effort: a file
	// locked by another process must not prevent the application from logging.
	_ = cleanupManagedLogs(dir, writer.path, crashPath, cfg.maxFiles)
	log.Printf("logger initialized -> %s (maxBytes=%d retention=%d crash=%s)", writer.path, cfg.maxBytes, cfg.maxFiles, crashPath)
}

// FlushFileLogger synchronizes the active application and crash sinks. It is
// safe to call from an application shutdown hook before CloseFileLogger.
func FlushFileLogger() error {
	fileLoggerMu.Lock()
	defer fileLoggerMu.Unlock()

	var errs []error
	if fileLogger != nil {
		if err := fileLogger.Sync(); err != nil {
			errs = append(errs, err)
		}
	}
	if logFile != nil {
		if err := logFile.Sync(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CloseFileLogger flushes and closes all logger-owned descriptors. The current
// main package does not own a Wails shutdown callback, so this function is
// exposed for the caller that owns that lifecycle; writes after Close are
// discarded by design. It is idempotent.
func CloseFileLogger() error {
	fileLoggerMu.Lock()
	defer fileLoggerMu.Unlock()

	// Stop new stdlib log writes from reaching a writer while it is closing.
	log.SetOutput(io.Discard)

	writer := fileLogger
	fileLogger = nil
	crash := logFile
	logFile = nil

	var errs []error
	if writer != nil {
		if err := writer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if crash != nil {
		if err := syncAndClose(crash); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// closeFileLogger preserves the existing shutdown helper name used by the
// desktop lifecycle integration. Keep the public variant above available for
// callers that can report a close error.
func closeFileLogger() {
	_ = CloseFileLogger()
}

func configureFallbackLogger(reason error) {
	fileLoggerMu.Lock()
	fileLogger = nil
	logFile = nil
	fileLoggerMu.Unlock()

	log.SetOutput(&redactingWriter{dst: os.Stderr})
	if reason != nil {
		log.Printf("file logger unavailable: %v", reason)
	}
}

func loadLoggerConfig() loggerConfig {
	return loggerConfig{
		maxBytes: envPositiveInt64(defaultLogMaxBytes, "GRAAL_RC_LOG_MAX_BYTES", "GRAAL_RC_LOG_MAX_SIZE"),
		maxFiles: envPositiveInt(defaultLogMaxFiles, "GRAAL_RC_LOG_RETENTION", "GRAAL_RC_LOG_MAX_FILES"),
	}
}

func envPositiveInt64(fallback int64, names ...string) int64 {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func envPositiveInt(fallback int, names ...string) int {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func envEnabled(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}

func resolveLogDir() (string, error) {
	var candidates []string
	if explicit := strings.TrimSpace(os.Getenv("GRAAL_RC_LOG_DIR")); explicit != "" {
		candidates = append(candidates, explicit)
	}

	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "graal-rc", "logs"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "graal-rc", "logs"))

	seen := make(map[string]struct{}, len(candidates))
	var errs []error
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if candidate == "." {
			continue
		}
		key := candidate
		if filepath.Separator == '\\' {
			key = strings.ToLower(key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if err := os.MkdirAll(candidate, 0o700); err != nil {
			errs = append(errs, fmt.Errorf("create log directory %q: %w", candidate, err))
			continue
		}
		info, err := os.Lstat(candidate)
		if err != nil {
			errs = append(errs, fmt.Errorf("stat log directory %q: %w", candidate, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Errorf("log directory %q is a symlink", candidate))
			continue
		}
		if !info.IsDir() {
			errs = append(errs, fmt.Errorf("log path %q is not a directory", candidate))
			continue
		}
		return candidate, nil
	}
	return "", errors.Join(errs...)
}

func newRotatingLogWriter(dir string, maxBytes int64, maxFiles int) (*rotatingLogWriter, error) {
	return newRotatingLogWriterWithClock(dir, maxBytes, maxFiles, time.Now)
}

func newRotatingLogWriterWithClock(dir string, maxBytes int64, maxFiles int, now func() time.Time) (*rotatingLogWriter, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("log directory is empty")
	}
	if maxBytes <= 0 {
		return nil, errors.New("log size limit must be positive")
	}
	if maxFiles <= 0 {
		return nil, errors.New("log retention must be positive")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}

	w := &rotatingLogWriter{
		dir:      filepath.Clean(dir),
		maxBytes: maxBytes,
		maxFiles: maxFiles,
		now:      now,
	}
	w.mu.Lock()
	err := w.openCurrentLocked(now())
	w.mu.Unlock()
	if err != nil {
		return nil, err
	}
	_ = cleanupManagedLogs(w.dir, w.path, "", w.maxFiles)
	return w, nil
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	data := []byte(redactLogMessage(string(p)))
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, errors.New("log writer is closed")
	}
	if err := w.ensureCurrentLocked(now); err != nil && w.file == nil {
		return 0, err
	}

	if w.file != nil && w.size > 0 && w.size > w.maxBytes-int64(len(data)) {
		// A rotation failure must not discard a diagnostic line. rotateLocked
		// reopens the old file on failure, so write to it even if the limit is
		// temporarily exceeded.
		if err := w.rotateLocked(now); err != nil && w.file == nil {
			return 0, err
		}
	}

	if w.file == nil {
		return 0, errors.New("log writer has no active file")
	}
	if err := writeAll(w.file, data); err != nil {
		return 0, err
	}
	w.size += int64(len(data))

	if w.mirror != nil {
		if err := writeAll(w.mirror, data); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *rotatingLogWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if w.closed {
			return nil
		}
		return errors.New("log writer has no active file")
	}
	return w.file.Sync()
}

func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.closeActiveLocked()
}

func (w *rotatingLogWriter) ensureCurrentLocked(now time.Time) error {
	date := logDate(now)
	if w.file == nil {
		return w.openCurrentLocked(now)
	}
	if w.date == date {
		return nil
	}

	closeErr := w.closeActiveLocked()
	openErr := w.openCurrentLocked(now)
	return errors.Join(closeErr, openErr)
}

func (w *rotatingLogWriter) openCurrentLocked(now time.Time) error {
	date := logDate(now)
	path := filepath.Join(w.dir, logFilePrefix+date+logFileSuffix)
	f, err := openManagedAppend(path)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.path = path
	w.date = date
	w.size = info.Size()
	return nil
}

func (w *rotatingLogWriter) closeActiveLocked() error {
	if w.file == nil {
		return nil
	}
	file := w.file
	w.file = nil
	var errs []error
	if err := file.Sync(); err != nil {
		errs = append(errs, err)
	}
	if err := file.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (w *rotatingLogWriter) rotateLocked(now time.Time) error {
	if w.file == nil {
		return w.openCurrentLocked(now)
	}

	oldPath := w.path
	oldDate := w.date
	closeErr := w.closeActiveLocked()
	if closeErr != nil {
		return errors.Join(closeErr, w.reopenCurrentLocked())
	}

	rotatedPath, err := nextRotatedLogPath(w.dir, oldDate)
	if err != nil {
		return errors.Join(err, w.reopenCurrentLocked())
	}
	if err := os.Rename(oldPath, rotatedPath); err != nil {
		return errors.Join(err, w.reopenCurrentLocked())
	}

	if err := w.openCurrentLocked(now); err != nil {
		// Roll back if creating the new active file failed. This preserves the
		// old segment and gives Write a chance to continue appending.
		rollbackErr := os.Rename(rotatedPath, oldPath)
		reopenErr := w.reopenCurrentLocked()
		return errors.Join(err, rollbackErr, reopenErr)
	}

	return cleanupManagedLogs(w.dir, w.path, w.crashPath, w.maxFiles)
}

func (w *rotatingLogWriter) reopenCurrentLocked() error {
	if w.path == "" {
		return errors.New("log writer has no active path")
	}
	f, err := openManagedAppend(w.path)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func openCrashLog(dir string, now time.Time) (*os.File, string, error) {
	name := logFilePrefix + "crash_" + logDate(now) + logFileSuffix
	path := filepath.Join(dir, name)
	f, err := openManagedAppend(path)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

func openManagedAppend(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing symlink log path %q", path)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("log path %q is not a regular file", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func nextRotatedLogPath(dir, date string) (string, error) {
	for sequence := 1; sequence <= 999999; sequence++ {
		name := fmt.Sprintf("%s%s_%03d%s", logFilePrefix, date, sequence, logFileSuffix)
		path, ok := safeManagedPath(dir, name)
		if !ok {
			return "", errors.New("invalid rotated log path")
		}
		_, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("too many rotated log segments")
}

type managedLogEntry struct {
	path    string
	name    string
	modTime time.Time
}

// cleanupManagedLogs only considers direct, regular files with names emitted
// by this logger. It never recurses, never follows a symlink, and ignores
// removal failures (for example a file held by another process on Windows).
func cleanupManagedLogs(dir, currentPath, crashPath string, maxFiles int) error {
	if maxFiles <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	protected := func(path string) bool {
		return sameLogPath(path, currentPath) ||
			sameLogPath(path, crashPath) ||
			isCurrentDayCrashPath(path)
	}

	files := make([]managedLogEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !isManagedLogName(name) {
			continue
		}
		path, ok := safeManagedPath(dir, name)
		if !ok {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, managedLogEntry{path: path, name: name, modTime: info.ModTime()})
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].name > files[j].name
		}
		return files[i].modTime.After(files[j].modTime)
	})

	remaining := maxFiles
	var errs []error
	for _, file := range files {
		if protected(file.path) {
			continue
		}
		if remaining > 0 {
			remaining--
			continue
		}
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove old log %q: %w", file.path, err))
		}
	}
	return errors.Join(errs...)
}

func safeManagedPath(dir, name string) (string, bool) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	base := filepath.Clean(dir)
	path := filepath.Join(base, name)
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Dir(rel) != "." {
		return "", false
	}
	return path, true
}

func isManagedLogName(name string) bool {
	if !strings.HasPrefix(name, logFilePrefix) || !strings.HasSuffix(name, logFileSuffix) {
		return false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileSuffix)
	if strings.HasPrefix(body, "crash_") {
		return validLogDate(strings.TrimPrefix(body, "crash_"))
	}
	parts := strings.Split(body, "_")
	if len(parts) != 3 && len(parts) != 4 {
		return false
	}
	if !validLogDate(strings.Join(parts[:3], "_")) {
		return false
	}
	return len(parts) == 3 || allDigits(parts[3])
}

func validLogDate(value string) bool {
	parsed, err := time.Parse("2006_01_02", value)
	return err == nil && parsed.Format("2006_01_02") == value
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func sameLogPath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func isCurrentDayCrashPath(path string) bool {
	name := filepath.Base(path)
	return name == logFilePrefix+"crash_"+logDate(time.Now())+logFileSuffix
}

func logDate(t time.Time) string {
	return t.Format("2006_01_02")
}

func syncAndClose(file *os.File) error {
	var errs []error
	if err := file.Sync(); err != nil {
		errs = append(errs, err)
	}
	if err := file.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func writeAll(dst io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := dst.Write(data)
		if n < 0 || n > len(data) {
			return errors.New("invalid log writer count")
		}
		data = data[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	data := []byte(redactLogMessage(string(p)))
	if err := writeAll(w.dst, data); err != nil {
		return 0, err
	}
	return len(p), nil
}

type redactionKind uint8

const (
	redactField redactionKind = iota
	redactToken
	redactHeader
	redactPayload
)

// redactLogMessage is intentionally applied at the final log boundary. This
// protects existing log.Printf call sites, including errors returned by the
// native library, without logging the original password/payload first.
func redactLogMessage(input string) string {
	if input == "" {
		return input
	}

	var output strings.Builder
	output.Grow(len(input))
	last := 0
	for i := 0; i < len(input); i++ {
		kind, valueStart, ok := sensitiveFieldAt(input, i)
		if !ok {
			continue
		}
		end := consumeLogValue(input, valueStart, kind)
		if end <= valueStart {
			continue
		}
		output.WriteString(input[last:valueStart])
		output.WriteString(redactedValue(input, valueStart, end, kind))
		last = end
		i = end - 1
	}
	output.WriteString(input[last:])
	return redactBearerAndKnownTokens(output.String())
}

func sensitiveFieldAt(input string, start int) (redactionKind, int, bool) {
	if start >= len(input) || !isLogIdentifierChar(input[start]) || (start > 0 && isLogIdentifierChar(input[start-1])) {
		return 0, 0, false
	}

	end := start + 1
	for end < len(input) && isLogIdentifierChar(input[end]) {
		end++
	}
	kind, ok := logRedactionKind(normalizeLogKey(input[start:end]))
	if !ok {
		return 0, 0, false
	}

	separator := end
	if separator+1 < len(input) && input[separator] == '\\' && input[separator+1] == '"' {
		separator += 2
	} else if separator < len(input) && (input[separator] == '"' || input[separator] == '\'') {
		separator++
	}
	for separator < len(input) && isLogSpace(input[separator]) {
		separator++
	}
	if separator >= len(input) || (input[separator] != ':' && input[separator] != '=') {
		return 0, 0, false
	}
	separator++
	for separator < len(input) && isLogSpace(input[separator]) {
		separator++
	}
	return kind, separator, true
}

func logRedactionKind(key string) (redactionKind, bool) {
	switch key {
	case "password", "passwd", "passphrase", "pass", "secret", "clientsecret", "privatekey", "credential", "credentials", "signature":
		return redactField, true
	case "token", "accesstoken", "refreshtoken", "idtoken", "authtoken", "apikey", "xapikey", "xauthtoken":
		return redactToken, true
	case "authorization", "proxyauthorization", "cookie", "setcookie":
		return redactHeader, true
	case "payload", "requestbody", "responsebody", "requestpayload", "responsepayload", "rawbody":
		return redactPayload, true
	default:
		return 0, false
	}
}

func normalizeLogKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return key
}

func isLogIdentifierChar(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-'
}

func isLogSpace(value byte) bool {
	return value == ' ' || value == '\t'
}

func consumeLogValue(input string, start int, kind redactionKind) int {
	if start >= len(input) {
		return start
	}
	if input[start] == '"' || input[start] == '\'' {
		return consumeQuotedLogValue(input, start, input[start])
	}
	if start+1 < len(input) && input[start] == '\\' && input[start+1] == '"' {
		return consumeEscapedQuotedLogValue(input, start)
	}
	if kind == redactPayload && (input[start] == '{' || input[start] == '[') {
		return consumeBalancedLogValue(input, start)
	}
	if kind == redactHeader {
		return consumeHeaderLogValue(input, start)
	}
	return consumeBareLogValue(input, start)
}

func consumeQuotedLogValue(input string, start int, quote byte) int {
	for i := start + 1; i < len(input); i++ {
		if input[i] == '\\' {
			i++
			continue
		}
		if input[i] == quote {
			return i + 1
		}
	}
	return len(input)
}

func consumeEscapedQuotedLogValue(input string, start int) int {
	var fallback int
	for i := start + 2; i+1 < len(input); i++ {
		if input[i] != '\\' || input[i+1] != '"' {
			continue
		}
		end := i + 2
		if fallback == 0 {
			fallback = end
		}
		probe := end
		for probe < len(input) && isLogSpace(input[probe]) {
			probe++
		}
		if probe >= len(input) || isLogValueDelimiter(input[probe]) {
			return end
		}
	}
	if fallback != 0 {
		return fallback
	}
	return len(input)
}

func consumeBalancedLogValue(input string, start int) int {
	open := input[start]
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	depth := 0
	var quote byte
	for i := start; i < len(input); i++ {
		if quote != 0 {
			if input[i] == '\\' {
				i++
				continue
			}
			if input[i] == quote {
				quote = 0
			}
			continue
		}
		if input[i] == '"' || input[i] == '\'' {
			quote = input[i]
			continue
		}
		if input[i] == open {
			depth++
		}
		if input[i] == close {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(input)
}

func consumeHeaderLogValue(input string, start int) int {
	for i := start; i < len(input); i++ {
		if isLogValueDelimiter(input[i]) || input[i] == '\n' || input[i] == '\r' {
			return i
		}
		if isLogSpace(input[i]) {
			probe := i
			for probe < len(input) && isLogSpace(input[probe]) {
				probe++
			}
			if isLogMapFieldStart(input, probe) {
				return i
			}
		}
	}
	return len(input)
}

func isLogMapFieldStart(input string, start int) bool {
	if start >= len(input) || !isLogIdentifierChar(input[start]) {
		return false
	}
	end := start + 1
	for end < len(input) && isLogIdentifierChar(input[end]) {
		end++
	}
	for end < len(input) && isLogSpace(input[end]) {
		end++
	}
	return end < len(input) && (input[end] == ':' || input[end] == '=')
}

func consumeBareLogValue(input string, start int) int {
	for i := start; i < len(input); i++ {
		if isLogSpace(input[i]) || isLogValueDelimiter(input[i]) {
			return i
		}
	}
	return len(input)
}

func isLogValueDelimiter(value byte) bool {
	return value == ',' || value == ';' || value == '&' || value == ']' || value == '}' || value == ')'
}

func redactedValue(input string, start, end int, kind redactionKind) string {
	marker := "[REDACTED]"
	if kind == redactToken {
		marker = "[REDACTED token]"
	} else if kind == redactHeader {
		marker = "[REDACTED header]"
	} else if kind == redactPayload {
		marker = "[REDACTED payload len=" + strconv.Itoa(end-start) + "]"
	}

	if input[start] == '"' || input[start] == '\'' {
		return string(input[start]) + marker + string(input[end-1])
	}
	if start+1 < end && input[start] == '\\' && input[start+1] == '"' {
		result := input[start : start+2]
		result += marker
		if end >= start+4 && input[end-2] == '\\' && input[end-1] == '"' {
			result += input[end-2 : end]
		}
		return result
	}
	return marker
}

func redactBearerAndKnownTokens(input string) string {
	input = redactAuthScheme(input, "Bearer")
	input = redactAuthScheme(input, "Basic")
	return redactOpaqueTokenPrefixes(input)
}

func redactAuthScheme(input, scheme string) string {
	var output strings.Builder
	output.Grow(len(input))
	last := 0
	for i := 0; i+len(scheme) <= len(input); i++ {
		if !strings.EqualFold(input[i:i+len(scheme)], scheme) || (i > 0 && isLogIdentifierChar(input[i-1])) || (i+len(scheme) < len(input) && isLogIdentifierChar(input[i+len(scheme)])) {
			continue
		}
		valueStart := i + len(scheme)
		for valueStart < len(input) && isLogSpace(input[valueStart]) {
			valueStart++
		}
		if valueStart == i+len(scheme) || valueStart >= len(input) {
			continue
		}
		valueEnd := consumeBareLogValue(input, valueStart)
		if valueEnd <= valueStart {
			continue
		}
		output.WriteString(input[last:valueStart])
		output.WriteString("[REDACTED token]")
		last = valueEnd
		i = valueEnd - 1
	}
	output.WriteString(input[last:])
	return output.String()
}

func redactOpaqueTokenPrefixes(input string) string {
	prefixes := []string{"github_pat_", "ghp_", "glpat-", "xoxb-", "xoxp-", "sk-"}
	var output strings.Builder
	output.Grow(len(input))
	last := 0
	for i := 0; i < len(input); i++ {
		if i > 0 && isLogIdentifierChar(input[i-1]) {
			continue
		}
		matched := ""
		for _, prefix := range prefixes {
			if i+len(prefix) <= len(input) && strings.EqualFold(input[i:i+len(prefix)], prefix) {
				matched = prefix
				break
			}
		}
		if matched == "" {
			continue
		}
		end := consumeBareLogValue(input, i)
		if end <= i+len(matched) {
			continue
		}
		output.WriteString(input[last:i])
		output.WriteString("[REDACTED token]")
		last = end
		i = end - 1
	}
	output.WriteString(input[last:])
	return output.String()
}
