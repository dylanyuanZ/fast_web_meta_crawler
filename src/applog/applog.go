// Package applog provides a global log module that writes all log output
// to a timestamped file under the specified log directory.
// Each run creates a new log file, making it easy to trace issues per session.
package applog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// logFile holds the open file handle so it can be closed on shutdown.
var logFile *os.File

// Init creates the log directory (if needed) and opens a new timestamped log
// file. It configures the standard library's default logger to write only to
// the log file (console output is suppressed).
//
// Log files are named: <logDir>/crawler_<YYYYMMDD>_<HHMMSS>.log
//
// The caller should defer Close() to flush and close the file.
func Init(logDir string) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir %s: %w", logDir, err)
	}

	filename := fmt.Sprintf("crawler_%s.log", time.Now().Format("20060102_150405"))
	path := filepath.Join(logDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}

	logFile = f

	// Write log output only to the log file (not to console).
	log.SetOutput(f)

	log.Printf("INFO: [applog] Log file created: %s", path)
	return nil
}

// Close flushes and closes the log file.
// Safe to call even if Init was never called.
func Close() {
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}
