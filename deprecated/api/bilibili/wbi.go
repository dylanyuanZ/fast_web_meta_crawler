package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/deprecated/api/httpclient"
)

// wbiSigner handles Bilibili's wbi signature generation.
// It fetches img_key and sub_key from the nav API and caches them.
type wbiSigner struct {
	mu     sync.Mutex
	imgKey string
	subKey string
	client *httpclient.Client
}

// globalWbiSigner is the singleton wbi signer instance (initialized in NewAuthorCrawler).
var globalWbiSigner *wbiSigner

// invalidateKeys clears the cached wbi keys, forcing a re-fetch on the next signURL call.
// This should be called when a -352 error is received, as it may indicate expired keys.
func (w *wbiSigner) invalidateKeys() {
	w.mu.Lock()
	w.imgKey = ""
	w.subKey = ""
	w.mu.Unlock()
	log.Printf("INFO: Wbi keys invalidated, will re-fetch on next request")
}

// mixinKeyEncTab is the fixed permutation table used by Bilibili's wbi algorithm.
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

// getMixinKey generates the mixin key from the concatenated raw key using the permutation table.
func getMixinKey(raw string) string {
	var result strings.Builder
	for _, idx := range mixinKeyEncTab {
		if idx < len(raw) {
			result.WriteByte(raw[idx])
		}
	}
	key := result.String()
	if len(key) > 32 {
		key = key[:32]
	}
	return key
}

// initKeys fetches img_key and sub_key from Bilibili's nav API.
func (w *wbiSigner) initKeys() error {
	// Use the shared HTTP client (with cookie jar) to fetch nav API.
	var navResp struct {
		Code int `json:"code"`
		Data struct {
			WbiImg struct {
				ImgURL string `json:"img_url"`
				SubURL string `json:"sub_url"`
			} `json:"wbi_img"`
		} `json:"data"`
	}

	body, err := w.client.Get(context.Background(), "https://api.bilibili.com/x/web-interface/nav")
	if err != nil {
		// Fallback to plain http.Get if client fails.
		resp, httpErr := http.Get("https://api.bilibili.com/x/web-interface/nav")
		if httpErr != nil {
			return fmt.Errorf("fetch nav API: %w (original: %v)", httpErr, err)
		}
		defer resp.Body.Close()
		if decErr := json.NewDecoder(resp.Body).Decode(&navResp); decErr != nil {
			return fmt.Errorf("parse nav response: %w", decErr)
		}
	} else {
		if err := json.Unmarshal(body, &navResp); err != nil {
			return fmt.Errorf("parse nav response: %w", err)
		}
	}

	// Extract key from URL path: e.g. "https://i0.hdslb.com/bfs/wbi/xxx.png" -> "xxx"
	imgKey := strings.TrimSuffix(path.Base(navResp.Data.WbiImg.ImgURL), path.Ext(navResp.Data.WbiImg.ImgURL))
	subKey := strings.TrimSuffix(path.Base(navResp.Data.WbiImg.SubURL), path.Ext(navResp.Data.WbiImg.SubURL))

	w.mu.Lock()
	w.imgKey = imgKey
	w.subKey = subKey
	w.mu.Unlock()

	log.Printf("INFO: Wbi keys initialized (img_key=%s, sub_key=%s)", imgKey, subKey)
	return nil
}

// signURL adds wbi signature parameters (w_rid, wts) to the given URL.
// Lazily initializes keys on first call.
func (w *wbiSigner) signURL(rawURL string) (string, error) {
	w.mu.Lock()
	needInit := w.imgKey == "" || w.subKey == ""
	w.mu.Unlock()

	if needInit {
		if err := w.initKeys(); err != nil {
			return "", fmt.Errorf("wbi init keys: %w", err)
		}
	}

	w.mu.Lock()
	mixinKey := getMixinKey(w.imgKey + w.subKey)
	w.mu.Unlock()

	// Parse the original URL.
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}

	// Add wts (current timestamp).
	params := u.Query()
	params.Set("wts", fmt.Sprintf("%d", time.Now().Unix()))

	// Sort parameters by key.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sorted query string using raw values (no URL encoding).
	// Bilibili's wbi algorithm uses raw values for both md5 calculation and the final URL.
	var queryParts []string
	for _, k := range keys {
		v := sanitizeWbiValue(params.Get(k))
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", k, v))
	}
	queryStr := strings.Join(queryParts, "&")

	// Calculate w_rid = md5(queryStr + mixinKey).
	hash := md5.Sum([]byte(queryStr + mixinKey))
	wRid := hex.EncodeToString(hash[:])

	// Append w_rid to the query.
	u.RawQuery = queryStr + "&w_rid=" + wRid
	return u.String(), nil
}

// sanitizeWbiValue removes characters that Bilibili's wbi algorithm filters out.
func sanitizeWbiValue(s string) string {
	unwanted := []string{"!", "'", "(", ")", "*"}
	result := s
	for _, ch := range unwanted {
		result = strings.ReplaceAll(result, ch, "")
	}
	return result
}
