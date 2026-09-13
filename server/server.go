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
	payloads *Payloads
	barrier  *barrier
}

type barrierConn struct {
	conn     net.Conn
	encoding string
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

func (b *barrier) wait(conn net.Conn, encoding string) {
	b.mu.Lock()
	done := b.done
	b.mu.Unlock()
	<-done
}

func NewServer(payloads *Payloads) *Server {
	return &Server{
		payloads: payloads,
		barrier:  newBarrier(),
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

func sendFile(conn net.Conn, payloads *Payloads, encoding string) error {
	fn := payloads.SelectFile(encoding)
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	slog.Info("sending payload", "file", fn)
	defer f.Close()
	_, err = (conn.(*net.TCPConn)).ReadFrom(f)
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
	for _, token := range strings.Split(strings.ToLower(string(val[:endIdx])), ",") {
		token = strings.TrimSpace(token)
		switch token {
		case "zstd":
			return "zstd"
		case "br":
			return "br"
		case "gzip":
			return "gzip"
		}
	}
	return "gzip"
}

func (s *Server) bomb(conn net.Conn, pAddr string) error {
	defer conn.Close()
	slog.Info("serving", "addr", (conn.(*net.TCPConn)).RemoteAddr())

	// Read up to 4096 bytes of HTTP headers to determine compression.
	// Discarded — only the Accept-Encoding header is needed.
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	encoding := detectEncoding(buf[:n])

	// Stall some to pretend the client request is being processed.
	time.Sleep(time.Duration((rand.Float64() + 0.01) * float64(time.Second)))

	// Randomly choose to redirect.
	var hdr []byte
	if rand.Intn(5) == 0 {
		hdr = hdr200(encoding)
	} else {
		tstr := fmt.Sprintf("%v", time.Now().UnixNano())
		hdr = hdr302(pAddr, tstr, encoding)
	}
	if _, err := conn.Write(hdr); err != nil {
		return err
	}

	// Barrier: wait 3 seconds for other connections, then send.
	s.barrier.wait(conn, encoding)
	if err := sendFile(conn, s.payloads, encoding); err != nil {
		return err
	}

	// Randomly sleep.
	slog.Info("served", "addr", (conn.(*net.TCPConn)).RemoteAddr())
	if rand.Intn(5) == 0 {
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
