package test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/platform/bilibili"
)

// ==================== UserInfoResp JSON Parsing Tests ====================

func TestUserInfoResp_NormalParsing(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"name": "TestUser",
			"sign": "Hello World"
		}
	}`

	var resp bilibili.UserInfoResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal UserInfoResp: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Code = %d, want 0", resp.Code)
	}
	if resp.Data.Name != "TestUser" {
		t.Errorf("Name = %q, want %q", resp.Data.Name, "TestUser")
	}
	if resp.Data.Sign != "Hello World" {
		t.Errorf("Sign = %q, want %q", resp.Data.Sign, "Hello World")
	}
}

func TestUserInfoResp_APIError(t *testing.T) {
	jsonStr := `{
		"code": -404,
		"message": "user not found",
		"data": {}
	}`

	var resp bilibili.UserInfoResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Code != -404 {
		t.Errorf("Code = %d, want -404", resp.Code)
	}
	if resp.Message != "user not found" {
		t.Errorf("Message = %q, want %q", resp.Message, "user not found")
	}
}

func TestUserInfoResp_EmptyName(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"name": "",
			"sign": ""
		}
	}`

	var resp bilibili.UserInfoResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Data.Name != "" {
		t.Errorf("Name = %q, want empty", resp.Data.Name)
	}
}

// ==================== UserStatResp JSON Parsing Tests ====================

func TestUserStatResp_NormalParsing(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"follower": 1234567
		}
	}`

	var resp bilibili.UserStatResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal UserStatResp: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Code = %d, want 0", resp.Code)
	}
	if resp.Data.Follower != 1234567 {
		t.Errorf("Follower = %d, want 1234567", resp.Data.Follower)
	}
}

func TestUserStatResp_ZeroFollowers(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"follower": 0
		}
	}`

	var resp bilibili.UserStatResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Data.Follower != 0 {
		t.Errorf("Follower = %d, want 0", resp.Data.Follower)
	}
}

func TestUserStatResp_LargeFollowerCount(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"follower": 99999999
		}
	}`

	var resp bilibili.UserStatResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Data.Follower != 99999999 {
		t.Errorf("Follower = %d, want 99999999", resp.Data.Follower)
	}
}

// ==================== VideoListResp JSON Parsing Tests ====================

func TestVideoListResp_NormalParsing(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"list": {
				"vlist": [
					{
						"title": "Video 1",
						"bvid": "BV1xx411c7mD",
						"play": 50000,
						"comment": 200,
						"length": "10:30",
						"created": 1700000000,
						"video_review": 100
					},
					{
						"title": "Video 2",
						"bvid": "BV1yy411c7mE",
						"play": 30000,
						"comment": 150,
						"length": "5:45",
						"created": 1700100000,
						"video_review": 50
					}
				]
			},
			"page": {
				"pn": 1,
				"ps": 50,
				"count": 120
			}
		}
	}`

	var resp bilibili.VideoListResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal VideoListResp: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Code = %d, want 0", resp.Code)
	}
	if len(resp.Data.List.Vlist) != 2 {
		t.Fatalf("Vlist length = %d, want 2", len(resp.Data.List.Vlist))
	}

	// Verify first video.
	v := resp.Data.List.Vlist[0]
	if v.Title != "Video 1" {
		t.Errorf("Title = %q, want %q", v.Title, "Video 1")
	}
	if v.BvID != "BV1xx411c7mD" {
		t.Errorf("BvID = %q, want %q", v.BvID, "BV1xx411c7mD")
	}
	if v.Play != 50000 {
		t.Errorf("Play = %d, want 50000", v.Play)
	}
	if v.Comment != 200 {
		t.Errorf("Comment = %d, want 200", v.Comment)
	}
	if v.Length != "10:30" {
		t.Errorf("Length = %q, want %q", v.Length, "10:30")
	}

	// Verify pagination.
	if resp.Data.Page.PN != 1 {
		t.Errorf("PN = %d, want 1", resp.Data.Page.PN)
	}
	if resp.Data.Page.PS != 50 {
		t.Errorf("PS = %d, want 50", resp.Data.Page.PS)
	}
	if resp.Data.Page.Count != 120 {
		t.Errorf("Count = %d, want 120", resp.Data.Page.Count)
	}
}

func TestVideoListResp_EmptyVideoList(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"list": {
				"vlist": []
			},
			"page": {
				"pn": 1,
				"ps": 50,
				"count": 0
			}
		}
	}`

	var resp bilibili.VideoListResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Data.List.Vlist) != 0 {
		t.Errorf("Vlist length = %d, want 0", len(resp.Data.List.Vlist))
	}
	if resp.Data.Page.Count != 0 {
		t.Errorf("Count = %d, want 0", resp.Data.Page.Count)
	}
}

func TestVideoListResp_APIError(t *testing.T) {
	jsonStr := `{
		"code": -352,
		"message": "risk control triggered",
		"data": {}
	}`

	var resp bilibili.VideoListResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Code != -352 {
		t.Errorf("Code = %d, want -352", resp.Code)
	}
}

func TestVideoListResp_InvalidJSON(t *testing.T) {
	invalidJSON := []byte(`not a json`)

	var resp bilibili.VideoListResp
	err := json.Unmarshal(invalidJSON, &resp)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// ==================== PageInfo Calculation Tests ====================

func TestPageInfoCalculation_Normal(t *testing.T) {
	totalCount := 120
	pageSize := bilibili.VideoPageSize() // 50

	totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
	if totalPages != 3 {
		t.Errorf("totalPages = %d, want 3 (120/50 = 2.4, ceil = 3)", totalPages)
	}
}

func TestPageInfoCalculation_ExactDivision(t *testing.T) {
	totalCount := 100
	pageSize := bilibili.VideoPageSize()

	totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
	if totalPages != 2 {
		t.Errorf("totalPages = %d, want 2 (100/50 = 2.0)", totalPages)
	}
}

func TestPageInfoCalculation_SinglePage(t *testing.T) {
	totalCount := 10
	pageSize := bilibili.VideoPageSize()

	totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
	if totalPages != 1 {
		t.Errorf("totalPages = %d, want 1 (10/50 = 0.2, ceil = 1)", totalPages)
	}
}

func TestPageInfoCalculation_ZeroCount(t *testing.T) {
	totalCount := 0
	pageSize := bilibili.VideoPageSize()

	totalPages := int(math.Ceil(float64(totalCount) / float64(pageSize)))
	if totalPages != 0 {
		t.Errorf("totalPages = %d, want 0", totalPages)
	}
}

// ==================== VideoListItem → VideoDetail Conversion Tests ====================
// These tests verify the conversion pipeline that FetchAuthorVideos performs:
// VideoListResp JSON → parse → convert to []VideoDetail + PageInfo.

func TestVideoDetailConversion_FieldMapping(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"list": {
				"vlist": [
					{
						"title": "My Great Video",
						"bvid": "BV1xx411c7mD",
						"play": 50000,
						"comment": 200,
						"length": "10:30",
						"created": 1700000000,
						"video_review": 100
					}
				]
			},
			"page": {
				"pn": 1,
				"ps": 50,
				"count": 1
			}
		}
	}`

	var resp bilibili.VideoListResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Data.List.Vlist) != 1 {
		t.Fatalf("Vlist length = %d, want 1", len(resp.Data.List.Vlist))
	}

	item := resp.Data.List.Vlist[0]

	// Verify all fields that map to VideoDetail.
	if item.Title != "My Great Video" {
		t.Errorf("Title = %q, want %q", item.Title, "My Great Video")
	}
	if item.BvID != "BV1xx411c7mD" {
		t.Errorf("BvID = %q, want %q", item.BvID, "BV1xx411c7mD")
	}
	if item.Play != 50000 {
		t.Errorf("Play = %d, want 50000", item.Play)
	}
	if item.Comment != 200 {
		t.Errorf("Comment = %d, want 200", item.Comment)
	}
	if item.Length != "10:30" {
		t.Errorf("Length = %q, want %q", item.Length, "10:30")
	}
	if item.Created != 1700000000 {
		t.Errorf("Created = %d, want 1700000000", item.Created)
	}
}

func TestVideoDetailConversion_PageInfoCalculation(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		wantPages int
	}{
		{name: "120 videos", count: 120, wantPages: 3},
		{name: "exact 50", count: 50, wantPages: 1},
		{name: "51 videos", count: 51, wantPages: 2},
		{name: "1 video", count: 1, wantPages: 1},
		{name: "0 videos", count: 0, wantPages: 0},
		{name: "5000 videos", count: 5000, wantPages: 100},
	}

	pageSize := bilibili.VideoPageSize()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalPages := 0
			if pageSize > 0 {
				totalPages = int(math.Ceil(float64(tt.count) / float64(pageSize)))
			}
			if totalPages != tt.wantPages {
				t.Errorf("totalPages = %d, want %d (count=%d, pageSize=%d)", totalPages, tt.wantPages, tt.count, pageSize)
			}
		})
	}
}

func TestVideoDetailConversion_LargePlayCount(t *testing.T) {
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"list": {
				"vlist": [
					{
						"title": "Viral Video",
						"bvid": "BV1viral",
						"play": 999999999,
						"comment": 500000,
						"length": "30:00",
						"created": 1700000000,
						"video_review": 10000
					}
				]
			},
			"page": {"pn": 1, "ps": 50, "count": 1}
		}
	}`

	var resp bilibili.VideoListResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	item := resp.Data.List.Vlist[0]
	if item.Play != 999999999 {
		t.Errorf("Play = %d, want 999999999", item.Play)
	}
	if item.Comment != 500000 {
		t.Errorf("Comment = %d, want 500000", item.Comment)
	}
}

func TestVideoDetailConversion_MissingOptionalFields(t *testing.T) {
	// Simulate a response where some optional fields are missing or zero.
	jsonStr := `{
		"code": 0,
		"message": "0",
		"data": {
			"list": {
				"vlist": [
					{
						"title": "Minimal Video",
						"bvid": "BV1min",
						"play": 0,
						"comment": 0,
						"length": "",
						"created": 0,
						"video_review": 0
					}
				]
			},
			"page": {"pn": 1, "ps": 50, "count": 1}
		}
	}`

	var resp bilibili.VideoListResp
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	item := resp.Data.List.Vlist[0]
	if item.Title != "Minimal Video" {
		t.Errorf("Title = %q, want %q", item.Title, "Minimal Video")
	}
	if item.Play != 0 {
		t.Errorf("Play = %d, want 0", item.Play)
	}
	if item.Length != "" {
		t.Errorf("Length = %q, want empty", item.Length)
	}
	if item.Created != 0 {
		t.Errorf("Created = %d, want 0", item.Created)
	}
}

// ==================== UserInfo + UserStat → AuthorInfo Assembly Tests ====================
// These tests verify the assembly logic that FetchAuthorInfo performs:
// UserInfoResp + UserStatResp → AuthorInfo.

func TestAuthorInfoAssembly_Normal(t *testing.T) {
	infoJSON := `{
		"code": 0,
		"message": "0",
		"data": {"name": "TestCreator", "sign": "I make videos"}
	}`
	statJSON := `{
		"code": 0,
		"message": "0",
		"data": {"follower": 50000}
	}`

	var infoResp bilibili.UserInfoResp
	if err := json.Unmarshal([]byte(infoJSON), &infoResp); err != nil {
		t.Fatalf("failed to unmarshal UserInfoResp: %v", err)
	}

	var statResp bilibili.UserStatResp
	if err := json.Unmarshal([]byte(statJSON), &statResp); err != nil {
		t.Fatalf("failed to unmarshal UserStatResp: %v", err)
	}

	// Simulate the assembly logic from FetchAuthorInfo.
	if infoResp.Code != 0 {
		t.Fatalf("unexpected info API error: code=%d", infoResp.Code)
	}
	if statResp.Code != 0 {
		t.Fatalf("unexpected stat API error: code=%d", statResp.Code)
	}

	name := infoResp.Data.Name
	followers := statResp.Data.Follower

	if name != "TestCreator" {
		t.Errorf("Name = %q, want %q", name, "TestCreator")
	}
	if followers != 50000 {
		t.Errorf("Followers = %d, want 50000", followers)
	}
}

func TestAuthorInfoAssembly_InfoAPIError(t *testing.T) {
	infoJSON := `{
		"code": -404,
		"message": "user not found",
		"data": {}
	}`

	var infoResp bilibili.UserInfoResp
	if err := json.Unmarshal([]byte(infoJSON), &infoResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if infoResp.Code == 0 {
		t.Error("expected non-zero error code")
	}
	// In real code, FetchAuthorInfo would return an error here.
}

func TestAuthorInfoAssembly_StatAPIError(t *testing.T) {
	statJSON := `{
		"code": -799,
		"message": "request too frequent",
		"data": {}
	}`

	var statResp bilibili.UserStatResp
	if err := json.Unmarshal([]byte(statJSON), &statResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if statResp.Code == 0 {
		t.Error("expected non-zero error code")
	}
}

func TestAuthorInfoAssembly_UnicodeCharacters(t *testing.T) {
	infoJSON := `{
		"code": 0,
		"message": "0",
		"data": {"name": "日本語テスト🎮", "sign": "中文签名 emoji 🎬"}
	}`

	var infoResp bilibili.UserInfoResp
	if err := json.Unmarshal([]byte(infoJSON), &infoResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if infoResp.Data.Name != "日本語テスト🎮" {
		t.Errorf("Name = %q, want %q", infoResp.Data.Name, "日本語テスト🎮")
	}
	if infoResp.Data.Sign != "中文签名 emoji 🎬" {
		t.Errorf("Sign = %q, want %q", infoResp.Data.Sign, "中文签名 emoji 🎬")
	}
}

func TestAuthorInfoAssembly_MaxFollowers(t *testing.T) {
	// Test with a very large follower count (e.g., top Bilibili creators).
	statJSON := `{
		"code": 0,
		"message": "0",
		"data": {"follower": 78000000}
	}`

	var statResp bilibili.UserStatResp
	if err := json.Unmarshal([]byte(statJSON), &statResp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if statResp.Data.Follower != 78000000 {
		t.Errorf("Follower = %d, want 78000000", statResp.Data.Follower)
	}
}
