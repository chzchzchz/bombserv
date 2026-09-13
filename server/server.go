package server

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"time"
)

var hdr200 = []byte("HTTP/1.1 200 OK\nContent-Type: text/html; charset=utf-8\r\nContent-Encoding: gzip\n\n")

func sendFile(conn net.Conn) error {
	fn := fmt.Sprintf("%dMB.gz", sizes[rand.Intn(len(sizes))])
	f, err := os.Open(fn)
	if err != nil {
		return err
	}
	log.Println("sending payload", fn)
	defer f.Close()
	_, err = (conn.(*net.TCPConn)).ReadFrom(f)
	return err
}

func bomb(conn net.Conn, pAddr string) error {
	defer conn.Close()
	log.Printf("serving %v", (conn.(*net.TCPConn)).RemoteAddr())

	// Show what the client sent over.
	go io.Copy(os.Stdout, conn)

	// Stall some to pretend the client request is being processed.
	time.Sleep(time.Duration((rand.Float64() + 0.01) * float64(time.Second)))

	// Randomly choose to redirect.
	var hdr []byte
	if rand.Intn(5) == 0 {
		hdr = hdr200
	} else {
		tstr := fmt.Sprintf("%v", time.Now().UnixNano())
		hdr302 := []byte("HTTP/1.1 302 Found\nLocation: http://" +
			pAddr + "/" + tstr + "\nContent-Type: text/html\r\nContent-Encoding: gzip\n\n")
		hdr = hdr302
	}
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	if err := sendFile(conn); err != nil {
		return err
	}

	// Randomly sleep.
	log.Printf("served %v", (conn.(*net.TCPConn)).RemoteAddr())
	if rand.Intn(5) == 0 {
		log.Printf("sleeping %v", (conn.(*net.TCPConn)).RemoteAddr())
		time.Sleep(20 * time.Second)
	}
	return nil
}

func Serve(ln net.Listener, pAddr string) error {
	log.Println("listening on", ln.Addr().String(), "with publish address", pAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go func() {
			if err := bomb(conn, pAddr); err != nil {
				log.Println("error:", err)
			}
		}()
	}
}
