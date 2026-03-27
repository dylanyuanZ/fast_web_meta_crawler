package bilibili

import "testing"

// ==================== stripHTMLTags Tests ====================

func TestStripHTMLTags_Normal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single em tag",
			input: `<em class="keyword">生化危机</em>9`,
			want:  "生化危机9",
		},
		{
			name:  "multiple em tags",
			input: `<em class="keyword">生化</em>危机<em class="keyword">9</em>`,
			want:  "生化危机9",
		},
		{
			name:  "nested tags",
			input: `<div><em>hello</em></div>`,
			want:  "hello",
		},
		{
			name:  "self-closing tag",
			input: `hello<br/>world`,
			want:  "helloworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTMLTags(tt.input)
			if got != tt.want {
				t.Errorf("stripHTMLTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripHTMLTags_NoTags(t *testing.T) {
	input := "plain text without tags"
	got := stripHTMLTags(input)
	if got != input {
		t.Errorf("stripHTMLTags(%q) = %q, want %q", input, got, input)
	}
}

func TestStripHTMLTags_Empty(t *testing.T) {
	got := stripHTMLTags("")
	if got != "" {
		t.Errorf("stripHTMLTags(\"\") = %q, want \"\"", got)
	}
}

func TestStripHTMLTags_OnlyTags(t *testing.T) {
	input := "<em></em><br/>"
	got := stripHTMLTags(input)
	if got != "" {
		t.Errorf("stripHTMLTags(%q) = %q, want \"\"", input, got)
	}
}

// ==================== parseDuration Tests ====================

func TestParseDuration_MinutesSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "normal mm:ss", input: "12:34", want: 754},
		{name: "zero minutes", input: "0:30", want: 30},
		{name: "zero seconds", input: "5:00", want: 300},
		{name: "single digit", input: "1:02", want: 62},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDuration(tt.input)
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDuration_HoursMinutesSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "normal hh:mm:ss", input: "1:02:03", want: 3723},
		{name: "zero hours", input: "0:12:34", want: 754},
		{name: "large hours", input: "10:00:00", want: 36000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDuration(tt.input)
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDuration_PureSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "pure number", input: "120", want: 120},
		{name: "zero", input: "0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDuration(tt.input)
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDuration_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty string", input: "", want: 0},
		{name: "invalid format", input: "abc", want: 0},
		{name: "too many colons", input: "1:2:3:4", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDuration(tt.input)
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ==================== VideoPageSize Tests ====================

func TestVideoPageSize(t *testing.T) {
	got := VideoPageSize()
	if got != 50 {
		t.Errorf("VideoPageSize() = %d, want 50", got)
	}
}
