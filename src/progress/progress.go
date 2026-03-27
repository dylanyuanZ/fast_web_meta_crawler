package progress

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	src "github.com/dylanyuanZ/fast_web_meta_crawler/src"
)

// Progress represents the checkpoint state for resumable crawling.
type Progress struct {
	Platform    string          `json:"platform"`
	Keyword     string          `json:"keyword"`
	Stage       int             `json:"stage"`
	SearchPages []int           `json:"search_pages"`
	AuthorMids  []src.AuthorMid `json:"author_mids"`
	DoneAuthors map[string]bool `json:"done_authors"`
	UpdatedAt   time.Time       `json:"updated_at"`

	mu sync.Mutex // protects concurrent writes
}

// taskHash computes the progress file hash: MD5(platform_keyword) first 8 chars.
func taskHash(platform, keyword string) string {
	h := md5.Sum([]byte(fmt.Sprintf("%s_%s", platform, keyword)))
	return fmt.Sprintf("%x", h)[:8]
}

// progressFileName returns the progress file name for the given task.
func progressFileName(platform, keyword string) string {
	return fmt.Sprintf("progress_%s.json", taskHash(platform, keyword))
}

// progressFilePath returns the full path to the progress file.
func progressFilePath(outputDir, platform, keyword string) string {
	return filepath.Join(outputDir, progressFileName(platform, keyword))
}

// Load reads and parses the progress file for the given platform and keyword.
// Returns nil if no progress file exists or validation fails.
func Load(outputDir, platform, keyword string) *Progress {
	path := progressFilePath(outputDir, platform, keyword)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		log.Printf("WARN: failed to read progress file %s: %v", path, err)
		return nil
	}

	p := &Progress{}
	if err := json.Unmarshal(data, p); err != nil {
		log.Printf("WARN: failed to parse progress file %s: %v", path, err)
		return nil
	}

	// Validate that the progress file matches the current task.
	if p.Platform != platform || p.Keyword != keyword {
		log.Printf("WARN: progress file mismatch (platform=%s/%s, keyword=%s/%s), ignoring",
			p.Platform, platform, p.Keyword, keyword)
		return nil
	}

	// Ensure DoneAuthors map is initialized.
	if p.DoneAuthors == nil {
		p.DoneAuthors = make(map[string]bool)
	}

	return p
}

// NewProgress creates a new Progress instance for the given task.
func NewProgress(platform, keyword string) *Progress {
	return &Progress{
		Platform:    platform,
		Keyword:     keyword,
		Stage:       0,
		SearchPages: make([]int, 0),
		AuthorMids:  make([]src.AuthorMid, 0),
		DoneAuthors: make(map[string]bool),
		UpdatedAt:   time.Now(),
	}
}

// Save writes the current progress to the progress file using atomic write (temp + rename).
func (p *Progress) Save(outputDir string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.UpdatedAt = time.Now()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}

	target := progressFilePath(outputDir, p.Platform, p.Keyword)
	tmpPath := target + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// AddSearchPage records a completed search page and persists the progress.
func (p *Progress) AddSearchPage(outputDir string, page int) error {
	p.mu.Lock()
	p.SearchPages = append(p.SearchPages, page)
	p.mu.Unlock()
	return p.Save(outputDir)
}

// SetAuthorMids sets the deduplicated author list (called after stage 0 completes).
func (p *Progress) SetAuthorMids(outputDir string, mids []src.AuthorMid) error {
	p.mu.Lock()
	p.AuthorMids = mids
	p.Stage = 1
	p.mu.Unlock()
	return p.Save(outputDir)
}

// MarkDone marks an author as completed in stage 1 and persists the progress.
func (p *Progress) MarkDone(outputDir string, mid string) error {
	p.mu.Lock()
	p.DoneAuthors[mid] = true
	p.mu.Unlock()
	return p.Save(outputDir)
}

// CompletedPages returns a set of completed page numbers for quick lookup.
func (p *Progress) CompletedPages() map[int]bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	set := make(map[int]bool, len(p.SearchPages))
	for _, page := range p.SearchPages {
		set[page] = true
	}
	return set
}

// PendingAuthors returns the list of authors not yet completed in stage 1.
func (p *Progress) PendingAuthors() []src.AuthorMid {
	p.mu.Lock()
	defer p.mu.Unlock()

	pending := make([]src.AuthorMid, 0)
	for _, mid := range p.AuthorMids {
		if !p.DoneAuthors[mid.ID] {
			pending = append(pending, mid)
		}
	}
	return pending
}

// Clean removes the progress file after task completion.
func Clean(outputDir, platform, keyword string) error {
	path := progressFilePath(outputDir, platform, keyword)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove progress file: %w", err)
	}
	return nil
}
