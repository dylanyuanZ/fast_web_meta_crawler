package browser

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// debugLog is a dedicated logger that writes DEBUG-level messages to a log file
// instead of stdout, keeping the console clean for INFO/WARN/FATAL messages.
// Initialized by InitDebugLog; if not initialized, debug messages are silently dropped.
var debugLog *log.Logger

// debugLogFile holds the open file handle so it can be closed on shutdown.
var debugLogFile *os.File

// InitDebugLog creates the log directory and opens a timestamped log file for
// debug-level browser network messages. Call this once during startup.
//
// Log files are written to: <logDir>/browser_debug_<timestamp>.log
// The caller should defer CloseDebugLog() to flush and close the file.
func InitDebugLog(logDir string) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir %s: %w", logDir, err)
	}

	filename := fmt.Sprintf("browser_debug_%s.log", time.Now().Format("20060102_150405"))
	path := filepath.Join(logDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open debug log file %s: %w", path, err)
	}

	debugLogFile = f
	debugLog = log.New(f, "", log.LstdFlags)

	log.Printf("INFO: [browser] Debug log file created: %s", path)
	return nil
}

// CloseDebugLog flushes and closes the debug log file.
// Safe to call even if InitDebugLog was never called.
func CloseDebugLog() {
	if debugLogFile != nil {
		debugLogFile.Close()
		debugLogFile = nil
		debugLog = nil
	}
}

// logDebug writes a debug message to the file-based logger.
// If the debug logger is not initialized, the message is silently dropped.
func logDebug(format string, args ...interface{}) {
	if debugLog != nil {
		debugLog.Printf(format, args...)
	}
}
