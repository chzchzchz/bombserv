package server

import "testing"

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
