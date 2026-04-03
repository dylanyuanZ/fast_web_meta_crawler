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

// ==================== ReadVideoCSVAuthors ====================

// ReadVideoCSVAuthors reads a video CSV file and extracts unique AuthorMid entries.
// authorIDCol and authorNameCol specify the column indices for author ID and name.
// This is the generic replacement for the old ReadVideoCSV — it only extracts what's needed
// for deduplication, without parsing all video fields.
// Returns an empty slice (not error) if the file does not exist.
func ReadVideoCSVAuthors(csvPath string, authorNameCol, authorIDCol int) ([]src.AuthorMid, error) {
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
	if _, err := r.Read(); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("read video CSV header: %w", err)
	}

	maxCol := authorIDCol
	if authorNameCol > maxCol {
		maxCol = authorNameCol
	}

	seen := make(map[string]bool)
	var mids []src.AuthorMid
	for {
		row, err := r.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("WARN: skipping malformed video CSV row: %v", err)
			continue
		}
		if len(row) <= maxCol {
			log.Printf("WARN: skipping short video CSV row: %d columns", len(row))
			continue
		}

		id := row[authorIDCol]
		if id != "" && !seen[id] {
			seen[id] = true
			mids = append(mids, src.AuthorMid{Name: row[authorNameCol], ID: id})
		}
	}

	log.Printf("INFO: Read %d unique authors from CSV: %s", len(mids), csvPath)
	return mids, nil
}

// ==================== CSVWriter (generic, operates on []string) ====================

// CSVWriter provides concurrent-safe, incremental CSV writing for generic row data.
// Each WriteRow/WriteRows call appends rows and flushes immediately.
type CSVWriter struct {
	f    *os.File
	w    *csv.Writer
	mu   sync.Mutex
	path string // absolute file path
}

// Compile-time interface check.
var _ src.CSVRowWriter = (*CSVWriter)(nil)

// NewCSVWriter creates a new CSV file with BOM and header row.
// Used for first-time runs. Caller must call Close() when done.
func NewCSVWriter(outputDir, platform, keyword, fileType string, header []string) (*CSVWriter, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	filename := GenerateFileName(platform, keyword, fileType)
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

	log.Printf("INFO: CSV created: %s", absPath)
	return &CSVWriter{f: f, w: w, path: absPath}, nil
}

// OpenCSVWriter opens an existing CSV file in append mode (no header written).
// Used for resuming interrupted runs. Caller must call Close() when done.
func OpenCSVWriter(existingPath string) (*CSVWriter, error) {
	f, err := os.OpenFile(existingPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open existing CSV: %w", err)
	}

	w := csv.NewWriter(f)

	absPath, err := filepath.Abs(existingPath)
	if err != nil {
		absPath = existingPath
	}

	log.Printf("INFO: CSV opened for append: %s", absPath)
	return &CSVWriter{f: f, w: w, path: absPath}, nil
}

// WriteRow appends one row to the CSV file. Concurrent-safe.
func (cw *CSVWriter) WriteRow(row []string) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.f == nil {
		return fmt.Errorf("write row: writer already closed")
	}

	if err := cw.w.Write(row); err != nil {
		return fmt.Errorf("write row: %w", err)
	}
	cw.w.Flush()
	if err := cw.w.Error(); err != nil {
		return fmt.Errorf("flush row: %w", err)
	}
	return nil
}

// WriteRows appends multiple rows to the CSV file. Concurrent-safe.
func (cw *CSVWriter) WriteRows(rows [][]string) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.f == nil {
		return fmt.Errorf("write rows: writer already closed")
	}

	for _, row := range rows {
		if err := cw.w.Write(row); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	cw.w.Flush()
	if err := cw.w.Error(); err != nil {
		return fmt.Errorf("flush rows: %w", err)
	}
	return nil
}

// FilePath returns the absolute path of the CSV file.
func (cw *CSVWriter) FilePath() string {
	return cw.path
}

// Close flushes the writer and closes the file. Idempotent — safe to call multiple times.
func (cw *CSVWriter) Close() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.f == nil {
		return nil
	}

	cw.w.Flush()
	err := cw.f.Close()
	cw.f = nil
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
