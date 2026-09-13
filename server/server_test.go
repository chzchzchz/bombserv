package server

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsGetRoot(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
		want bool
	}{
		{"simple GET /", []byte("GET / HTTP/1.1\r\n"), true},
		{"GET / with query", []byte("GET /?abc=def HTTP/1.1\r\n"), true},
		{"GET / with multiple query params", []byte("GET /?abc=def&aaa=123 HTTP/1.1\r\n"), true},
		{"GET /index.html", []byte("GET /index.html HTTP/1.1\r\n"), false},
		{"POST /", []byte("POST / HTTP/1.1\r\n"), false},
		{"GET /foo", []byte("GET /foo HTTP/1.1\r\n"), false},
		{"no CRLF", []byte("GET / HTTP/1.1\n"), true},
		{"no CRLF with query", []byte("GET /?x=1 HTTP/1.1\n"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGetRoot(tt.buf)
			if got != tt.want {
				t.Errorf("isGetRoot(%q) = %v, want %v", tt.buf, got, tt.want)
			}
		})
	}
}

func TestPathFromRequest(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
		want string
	}{
		{"simple GET /", []byte("GET / HTTP/1.1\r\n"), "/"},
		{"GET /file.json", []byte("GET /file.json HTTP/1.1\r\n"), "/file.json"},
		{"GET /file.json with query", []byte("GET /file.json?x=1 HTTP/1.1\r\n"), "/file.json?x=1"},
		{"GET /dir/file.json", []byte("GET /dir/file.json HTTP/1.1\r\n"), "/dir/file.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathFromRequest(tt.buf)
			if got != tt.want {
				t.Errorf("pathFromRequest(%q) = %q, want %q", tt.buf, got, tt.want)
			}
		})
	}
}

func TestJSONDetection(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"json file", "/file.json", true},
		{"json in path", "/dir/file.json", true},
		{"raw path", "/file", false},
		{"index", "/", false},
		{"html file", "/file.html", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.HasSuffix(tt.path, ".json")
			if got != tt.want {
				t.Errorf("HasSuffix(%q, .json) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestEncodingToExt(t *testing.T) {
	tests := []struct {
		name string
		enc  string
		want string
	}{
		{"gzip", "gzip", "gz"},
		{"zstd", "zstd", "zst"},
		{"br", "br", "br"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodingToExt(tt.enc)
			if got != tt.want {
				t.Errorf("encodingToExt(%q) = %q, want %q", tt.enc, got, tt.want)
			}
		})
	}
}

func TestGenerateJSON(t *testing.T) {
	// 1MB produces n = 1*1024*1024/8 = 131072 nesting levels
	var buf bytes.Buffer
	GenerateJSON(&buf, 1)
	if buf.Len() != 1*1024*1024 {
		t.Errorf("generateJSON(1) size = %d, want %d", buf.Len(), 1*1024*1024)
	}
	if !strings.HasPrefix(buf.String(), `{"a":[`) {
		t.Errorf("generateJSON(1) does not start with {\"a\":[")
	}
	if !strings.HasSuffix(buf.String(), "]}") {
		t.Errorf("generateJSON(1) does not end with ]}")
	}
}
