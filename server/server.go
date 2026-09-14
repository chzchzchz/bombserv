package server

import (
	"bytes"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type Server struct {
	payloads  *Payloads
	barrier   *barrier
	indexPath string
}

type barrier struct {
	mu       sync.Mutex
	done     chan struct{}
	waitTime time.Duration
}

func newBarrier(waitTime time.Duration) *barrier {
	b := &barrier{done: make(chan struct{}), waitTime: waitTime}
	go b.monitor()
	return b
}

func (b *barrier) monitor() {
	for {
		time.Sleep(b.waitTime)
		b.mu.Lock()
		c := b.done
		nextc := make(chan struct{})
		b.done = nextc
		b.mu.Unlock()
		close(c)
	}
}

func (b *barrier) wait(conn net.Conn) {
	b.mu.Lock()
	done := b.done
	b.mu.Unlock()
	<-done
}

func NewServer(payloads *Payloads, indexPath string, barrierWait time.Duration) *Server {
	return &Server{
		payloads:  payloads,
		barrier:   newBarrier(barrierWait),
		indexPath: indexPath,
	}
}

func makeHeader(status, pAddr, tstr, encoding, contentType string, contentLength int) []byte {
	loc := ""
	if pAddr != "" {
		loc = fmt.Sprintf("Location: http://%s/%s\n", pAddr, tstr)
	}
	return []byte(fmt.Sprintf("HTTP/1.1 %s\n%sContent-Type: %s\r\nContent-Encoding: %s\r\nContent-Length: %d\r\n\r\n", status, loc, contentType, encoding, contentLength))
}

func hdr200(encoding, contentType string, contentLength int) []byte {
	return []byte(fmt.Sprintf("HTTP/1.1 200 OK\nContent-Type: %s\r\nContent-Encoding: %s\r\nContent-Length: %d\r\n\r\n", contentType, encoding, contentLength))
}

func hdr302(pAddr string, tstr string, encoding, contentType string) []byte {
	return makeHeader("302 Found", pAddr, tstr, encoding, contentType, 0)
}

func sendFile(conn net.Conn, trimFn string, last4 []byte, waitf func()) error {
	addr := (conn.(*net.TCPConn)).RemoteAddr().String()
	f, err := os.Open(trimFn)
	if err != nil {
		slog.Error("sendFile open error", "file", trimFn, "addr", addr, "err", err)
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		slog.Error("sendFile stat error", "file", trimFn, "addr", addr, "err", err)
		f.Close()
		return err
	}
	slog.Info("sending payload", "file", trimFn, "addr", addr, "bytes", fi.Size(), "last4", len(last4))
	defer f.Close()
	written, err := (conn.(*net.TCPConn)).ReadFrom(f)
	if err != nil {
		slog.Error("sendFile ReadFrom error", "file", trimFn, "addr", addr, "bytes_written", written, "err", err)
		return err
	}
	slog.Info("sendFile body sent", "file", trimFn, "addr", addr, "bytes_written", written)
	waitf()
	slog.Info("sendFile barrier released", "file", trimFn, "addr", addr)
	_, err = conn.Write(last4)
	if err != nil {
		slog.Error("sendFile Write(last4) error", "addr", addr, "err", err)
		return err
	}
	slog.Info("sendFile last4 sent", "addr", addr, "bytes", len(last4))
	return nil
}

func encodingToExt(encoding string) string {
	switch encoding {
	case "gzip":
		return "gz"
	case "zstd":
		return "zst"
	default:
		return encoding
	}
}

func detectEncoding(buf []byte) string {
	idx := bytes.Index(buf, []byte("Accept-Encoding:"))
	if idx == -1 {
		slog.Info("detectEncoding", "result", "gzip", "reason", "no Accept-Encoding header")
		return "gzip"
	}
	val := buf[idx+len("Accept-Encoding"):]
	endIdx := bytes.IndexByte(val, '\n')
	if endIdx == -1 {
		endIdx = len(val)
	}
	if endIdx > 0 && val[endIdx-1] == '\r' {
		endIdx--
	}
	seen := make(map[string]bool)
	for _, token := range strings.Split(strings.ToLower(string(val[:endIdx])), ",") {
		seen[strings.TrimSpace(token)] = true
	}
	if seen["br"] {
		slog.Info("detectEncoding", "result", "br", "header", string(val[:endIdx]))
		return "br"
	}
	if seen["zstd"] {
		slog.Info("detectEncoding", "result", "zstd", "header", string(val[:endIdx]))
		return "zstd"
	}
	if seen["gzip"] {
		slog.Info("detectEncoding", "result", "gzip", "header", string(val[:endIdx]))
		return "gzip"
	}
	slog.Info("detectEncoding", "result", "gzip", "reason", "no known encoding found")
	return "gzip"
}

func isGetRoot(buf []byte) bool {
	endIdx := bytes.IndexByte(buf, '\n')
	if endIdx == -1 {
		endIdx = len(buf)
	}
	if endIdx > 0 && buf[endIdx-1] == '\r' {
		endIdx--
	}
	line := strings.TrimRight(string(buf[:endIdx]), "\r")
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx == -1 || !strings.HasPrefix(line, "GET ") {
		return false
	}
	secondSpace := strings.IndexByte(line[spaceIdx+1:], ' ')
	if secondSpace == -1 {
		return false
	}
	path := line[spaceIdx+1 : spaceIdx+1+secondSpace]
	qIdx := strings.IndexByte(path, '?')
	if qIdx != -1 {
		path = path[:qIdx]
	}
	return path == "/"
}

func (s *Server) serveIndex(conn net.Conn) error {
	f, err := os.Open(s.indexPath)
	if err != nil {
		slog.Error("serveIndex open error", "path", s.indexPath, "err", err)
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		slog.Error("serveIndex stat error", "path", s.indexPath, "err", err)
		return err
	}
	hdr := []byte(fmt.Sprintf("HTTP/1.1 200 OK\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n", fi.Size()))
	if _, err := conn.Write(hdr); err != nil {
		slog.Error("serveIndex conn.Write(header) error", "err", err)
		return err
	}
	_, err = (conn.(*net.TCPConn)).ReadFrom(f)
	if err != nil {
		slog.Error("serveIndex ReadFrom error", "err", err)
	}
	return err
}

func contentTypeForKind(kind string) string {
	switch kind {
	case "json":
		return "application/json"
	default:
		return "text/html"
	}
}

func pathFromRequest(buf []byte) string {
	endIdx := bytes.IndexByte(buf, '\n')
	if endIdx == -1 {
		endIdx = len(buf)
	}
	if endIdx > 0 && buf[endIdx-1] == '\r' {
		endIdx--
	}
	line := strings.TrimRight(string(buf[:endIdx]), "\r")
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx == -1 {
		return ""
	}
	secondSpace := strings.IndexByte(line[spaceIdx+1:], ' ')
	if secondSpace == -1 {
		return ""
	}
	return line[spaceIdx+1 : spaceIdx+1+secondSpace]
}

func (s *Server) bomb(conn net.Conn, pAddr string) error {
	addr := (conn.(*net.TCPConn)).RemoteAddr().String()
	slog.Info("serving", "addr", addr)

	// Read up to 4096 bytes of HTTP headers.
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		slog.Error("conn.Read error", "addr", addr, "err", err)
		return err
	}
	slog.Info("headers", "addr", addr, "bytes", n, "data", string(buf[:n]))

	// Check for GET / index request.
	path := pathFromRequest(buf[:n])
	if s.indexPath != "" && path == "/" {
		slog.Info("serving index", "addr", addr, "path", path)
		return s.serveIndex(conn)
	}

	encoding := detectEncoding(buf[:n])
	kind := "raw"
	if idx := strings.IndexByte(path, '?'); idx != -1 {
		path = path[:idx]
	}
	if strings.HasSuffix(path, ".json") {
		kind = "json"
	}
	slog.Info("payload selection", "addr", addr, "encoding", encoding, "kind", kind, "path", path)

	// Stall some to pretend the client request is being processed.
	time.Sleep(time.Duration((rand.Float64() + 0.01) * float64(time.Second)))

	// Select payload to compute content length.
	trimFn, last4 := s.payloads.SelectPayload(encodingToExt(encoding), kind)
	tf, err := os.Open(trimFn)
	if err != nil {
		slog.Error("open trim file error", "addr", addr, "trimFn", trimFn, "err", err)
		return err
	}
	fi, _ := tf.Stat()
	tf.Close()
	contentLength := int(fi.Size()) + len(last4)

	// Randomly choose to redirect.
	var hdr []byte
	contentType := contentTypeForKind(kind)
	if rand.Intn(2) == 0 {
		hdr = hdr200(encoding, contentType, contentLength)
		slog.Info("sending 200", "addr", addr, "encoding", encoding, "kind", kind, "contentLength", contentLength)
	} else {
		tstr := fmt.Sprintf("%v", time.Now().UnixNano())
		hdr = hdr302(pAddr, tstr, encoding, contentType)
		slog.Info("sending 302", "addr", addr, "encoding", encoding, "kind", kind, "location", tstr)
	}
	if _, err := conn.Write(hdr); err != nil {
		slog.Error("conn.Write(header) error", "addr", addr, "encoding", encoding, "kind", kind, "err", err)
		return err
	}

	// Barrier: wait for other connections, then send.
	var waitf func()
	randomSleep := rand.Intn(10) == 0
	if randomSleep {
		waitf = func() {}
		slog.Info("barrier skipped", "addr", addr)
	} else {
		waitf = func() { s.barrier.wait(conn) }
		slog.Info("barrier waiting", "addr", addr)
	}

	slog.Info("sending payload", "addr", addr, "encoding", encoding, "kind", kind, "trimFn", trimFn, "last4", len(last4))
	if err := sendFile(conn, trimFn, last4, waitf); err != nil {
		slog.Error("sendFile error", "addr", addr, "encoding", encoding, "kind", kind, "err", err)
		return err
	}

	// Randomly sleep.
	slog.Info("served", "addr", addr, "encoding", encoding, "kind", kind)
	if randomSleep {
		slog.Info("sleeping", "addr", addr)
		time.Sleep(20 * time.Second)
	}
	return nil
}

func (s *Server) Serve(ln net.Listener, pAddr string) error {
	slog.Info("listening", "addr", ln.Addr().String(), "publish", pAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Error("accept error", "err", err)
			continue
		}
		go func() {
			defer conn.Close()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("bomb panic", "addr", (conn.(*net.TCPConn)).RemoteAddr(), "recovered", r)
				}
			}()
			if err := s.bomb(conn, pAddr); err != nil {
				slog.Error("bomb error", "err", err)
			}
			slog.Info("connection closed", "addr", (conn.(*net.TCPConn)).RemoteAddr())
		}()
	}
}
