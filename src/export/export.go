package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// GenerateFileName creates a CSV filename in the format: {platform}_{keyword}_{date}_{time}_{type}.csv
func GenerateFileName(platform, keyword, fileType string) string {
	now := time.Now()
	return fmt.Sprintf("%s_%s_%s_%s.csv",
		platform,
		keyword,
		now.Format("20060102"),
		now.Format("150405")+"_"+fileType,
	)
}

// ==================== VideoCSVWriter (incremental append) ====================

// VideoCSVWriter provides concurrent-safe, incremental CSV writing for video data.
// Each WriteRows call appends multiple rows and flushes immediately.
type VideoCSVWriter struct {
	f     *os.File
	w     *csv.Writer
	mu    sync.Mutex
	path  string                   // absolute file path
	toRow func(src.Video) []string // platform-specific row conversion
}

// NewVideoCSVWriter creates a new video CSV file with BOM and header row.
// header and toRow are provided by the platform-specific CSV adapter.
// Used for first-time runs. Caller must call Close() when done.
func NewVideoCSVWriter(outputDir, platform, keyword string,
	header []string, toRow func(src.Video) []string) (*VideoCSVWriter, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	filename := GenerateFileName(platform, keyword, "videos")
	fullPath := filepath.Join(outputDir, filename)

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	// Write UTF-8 BOM for Excel compatibility.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		f.Close()
		return nil, fmt.Errorf("write BOM: %w", err)
	}

	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		f.Close()
		return nil, fmt.Errorf("write header: %w", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return nil, fmt.Errorf("flush header: %w", err)
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		absPath = fullPath
	}

	log.Printf("INFO: Video CSV created: %s", absPath)
	return &VideoCSVWriter{f: f, w: w, path: absPath, toRow: toRow}, nil
}

// OpenVideoCSVWriter opens an existing video CSV file in append mode (no header written).
// toRow is provided by the platform-specific CSV adapter.
// Used for resuming interrupted runs. Caller must call Close() when done.
func OpenVideoCSVWriter(existingPath string, toRow func(src.Video) []string) (*VideoCSVWriter, error) {
	f, err := os.OpenFile(existingPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open existing video CSV: %w", err)
	}

	w := csv.NewWriter(f)

	absPath, err := filepath.Abs(existingPath)
	if err != nil {
		absPath = existingPath
	}

	log.Printf("INFO: Video CSV opened for append: %s", absPath)
	return &VideoCSVWriter{f: f, w: w, path: absPath, toRow: toRow}, nil
}

// WriteRows appends multiple video rows to the CSV file. Concurrent-safe.
// Flushes immediately after writing to ensure data persistence.
func (vw *VideoCSVWriter) WriteRows(videos []src.Video) error {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	if vw.f == nil {
		return fmt.Errorf("write rows: writer already closed")
	}

	for _, v := range videos {
		if err := vw.w.Write(vw.toRow(v)); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	vw.w.Flush()
	if err := vw.w.Error(); err != nil {
		return fmt.Errorf("flush rows: %w", err)
	}
	return nil
}

// FilePath returns the absolute path of the CSV file.
func (vw *VideoCSVWriter) FilePath() string {
	return vw.path
}

// Close flushes the writer and closes the file. Idempotent — safe to call multiple times.
func (vw *VideoCSVWriter) Close() error {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	if vw.f == nil {
		return nil
	}

	vw.w.Flush()
	err := vw.f.Close()
	vw.f = nil
	return err
}

// ==================== ReadVideoCSV ====================

// ReadVideoCSV reads a video CSV file and returns all video records.
// Used for resuming: reads existing videos from CSV to deduplicate author mids.
// Returns an empty slice (not error) if the file does not exist.
func ReadVideoCSV(csvPath string) ([]src.Video, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open video CSV for reading: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)

	// Read and discard the header row.
	// Note: Go's csv.Reader does NOT strip BOM, so header[0] may contain BOM bytes.
	// This is fine because we skip the header entirely.
	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("read video CSV header: %w", err)
	}

	// Validate header has at least 7 columns.
	if len(header) < 7 {
		return nil, fmt.Errorf("invalid video CSV header: expected at least 7 columns, got %d", len(header))
	}

	var videos []src.Video
	for {
		row, err := r.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("WARN: skipping malformed video CSV row: %v", err)
			continue
		}
		if len(row) < 7 {
			log.Printf("WARN: skipping short video CSV row: %d columns", len(row))
			continue
		}

		// Parse fields. Only Author and AuthorID are strictly needed for dedup,
		// but we parse all fields for completeness.
		video := src.Video{
			Title:    row[0],
			Author:   row[1],
			AuthorID: row[2],
			Source:   row[6],
		}

		// Parse PlayCount (best effort).
		if _, err := fmt.Sscanf(row[3], "%d", &video.PlayCount); err != nil {
			log.Printf("WARN: failed to parse PlayCount from video CSV: %v", err)
		}

		// Parse PubDate (best effort).
		if t, err := time.Parse("2006-01-02 15:04:05", row[4]); err == nil {
			video.PubDate = t
		}

		// Parse Duration (best effort).
		if _, err := fmt.Sscanf(row[5], "%d", &video.Duration); err != nil {
			log.Printf("WARN: failed to parse Duration from video CSV: %v", err)
		}

		videos = append(videos, video)
	}

	log.Printf("INFO: Read %d videos from CSV: %s", len(videos), csvPath)
	return videos, nil
}

// ==================== AuthorCSVWriter (incremental append) ====================

// AuthorCSVWriter provides concurrent-safe, incremental CSV writing for author data.
// Each WriteRow call appends one row and flushes immediately to ensure data persistence.
type AuthorCSVWriter struct {
	f     *os.File
	w     *csv.Writer
	mu    sync.Mutex
	path  string                    // absolute file path
	toRow func(src.Author) []string // platform-specific row conversion
}

// NewAuthorCSVWriter creates a new author CSV file with BOM and header row.
// header and toRow are provided by the platform-specific CSV adapter.
// Used for first-time runs. Caller must call Close() when done.
func NewAuthorCSVWriter(outputDir, platform, keyword string,
	header []string, toRow func(src.Author) []string) (*AuthorCSVWriter, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	filename := GenerateFileName(platform, keyword, "authors")
	fullPath := filepath.Join(outputDir, filename)

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	// Write UTF-8 BOM for Excel compatibility.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		f.Close()
		return nil, fmt.Errorf("write BOM: %w", err)
	}

	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		f.Close()
		return nil, fmt.Errorf("write header: %w", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return nil, fmt.Errorf("flush header: %w", err)
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		absPath = fullPath // fallback to relative path
	}

	log.Printf("INFO: Author CSV created: %s", absPath)
	return &AuthorCSVWriter{f: f, w: w, path: absPath, toRow: toRow}, nil
}

// OpenAuthorCSVWriter opens an existing author CSV file in append mode (no header written).
// toRow is provided by the platform-specific CSV adapter.
// Used for resuming interrupted runs. Caller must call Close() when done.
func OpenAuthorCSVWriter(existingPath string, toRow func(src.Author) []string) (*AuthorCSVWriter, error) {
	f, err := os.OpenFile(existingPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open existing CSV: %w", err)
	}

	w := csv.NewWriter(f)

	absPath, err := filepath.Abs(existingPath)
	if err != nil {
		absPath = existingPath
	}

	log.Printf("INFO: Author CSV opened for append: %s", absPath)
	return &AuthorCSVWriter{f: f, w: w, path: absPath, toRow: toRow}, nil
}

// WriteRow appends one author row to the CSV file. Concurrent-safe.
// Flushes immediately after writing to ensure data persistence.
func (aw *AuthorCSVWriter) WriteRow(author src.Author) error {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	if aw.f == nil {
		return fmt.Errorf("write row: writer already closed")
	}

	if err := aw.w.Write(aw.toRow(author)); err != nil {
		return fmt.Errorf("write row: %w", err)
	}
	aw.w.Flush()
	if err := aw.w.Error(); err != nil {
		return fmt.Errorf("flush row: %w", err)
	}
	return nil
}

// FilePath returns the absolute path of the CSV file.
func (aw *AuthorCSVWriter) FilePath() string {
	return aw.path
}

// Close flushes the writer and closes the file. Idempotent — safe to call multiple times.
func (aw *AuthorCSVWriter) Close() error {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	if aw.f == nil {
		return nil
	}

	aw.w.Flush()
	err := aw.f.Close()
	aw.f = nil
	return err
}

// ==================== ReadCompletedAuthors ====================

// ReadCompletedAuthors reads an author CSV file and returns a set of completed author IDs.
// The ID is read from column index 1 (the "ID" column).
// Returns an empty map (not error) if the file does not exist.
func ReadCompletedAuthors(csvPath string) (map[string]bool, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]bool), nil
		}
		return nil, fmt.Errorf("open CSV for reading: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)

	// Read and discard the header row.
	// Note: Go's csv.Reader does NOT strip BOM, so header[0] may contain BOM bytes.
	// This is fine because we only use column index 1 (ID), not index 0.
	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			return make(map[string]bool), nil
		}
		return nil, fmt.Errorf("read CSV header: %w", err)
	}

	// Validate header has at least 2 columns (Name + ID).
	if len(header) < 2 {
		return nil, fmt.Errorf("invalid CSV header: expected at least 2 columns, got %d", len(header))
	}

	completed := make(map[string]bool)
	for {
		row, err := r.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("WARN: skipping malformed CSV row: %v", err)
			continue
		}
		if len(row) >= 2 && row[1] != "" {
			completed[row[1]] = true
		}
	}

	log.Printf("INFO: Read %d completed authors from CSV: %s", len(completed), csvPath)
	return completed, nil
}
