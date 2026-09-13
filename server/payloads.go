package server

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"os"
	"sync"
)

var sizes = []int{128, 256, 512 /*, 1024, 2048, 4096, 8192*/}

func MakePayloads() {
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

func makePayload(mb int) []byte {
	log.Printf("making payload size %dMB\n", mb)
	var w bytes.Buffer
	gz, err := gzip.NewWriterLevel(&w, gzip.BestCompression)
	if err != nil {
		panic(err)
	}
	defer gz.Close()
	// Fill up a page's worth + plus overflow.
	trashWord, trashBuf := []byte("&#x1f33d;"), []byte("<html><head><title>")
	for len(trashBuf) < 4096 {
		trashBuf = append(trashBuf, trashWord...)
	}
	for i := 0; i < (mb*1024*1024)/4096; i++ {
		gz.Write(trashBuf)
	}
	gz.Write([]byte("</title></head><body><a href=\"http://corn.cash:8080/BOTS\">corn</a></body></html>"))
	if err := gz.Flush(); err != nil {
		panic(err)
	}
	log.Printf("payload size %dMB: %d bytes\n", mb, len(w.Bytes()))
	return w.Bytes()
}
