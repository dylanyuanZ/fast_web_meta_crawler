package browser

import "testing"

// ==================== extractDomain Tests ====================

func TestExtractDomain_Normal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "https with www",
			input: "https://www.bilibili.com",
			want:  ".bilibili.com",
		},
		{
			name:  "https without www",
			input: "https://space.bilibili.com",
			want:  ".space.bilibili.com",
		},
		{
			name:  "http protocol",
			input: "http://www.bilibili.com",
			want:  ".bilibili.com",
		},
		{
			name:  "with path",
			input: "https://www.bilibili.com/video/BV123",
			want:  ".bilibili.com",
		},
		{
			name:  "with port",
			input: "https://www.bilibili.com:443/path",
			want:  ".bilibili.com",
		},
		{
			name:  "bare domain",
			input: "bilibili.com",
			want:  ".bilibili.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomain(tt.input)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractDomain_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "already has leading dot",
			input: "https://.bilibili.com",
			want:  ".bilibili.com",
		},
		{
			name:  "localhost",
			input: "http://localhost:8080",
			want:  ".localhost",
		},
		{
			name:  "ip address",
			input: "http://192.168.1.1:8080/path",
			want:  ".192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDomain(tt.input)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
