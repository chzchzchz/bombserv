package server

import (
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

type Payloads struct {
	gzipSizes   []int
	zstdSizes   []int
	brotliSizes []int
	last4       map[string][]byte
}

func MakePayloads() *Payloads {
	p := &Payloads{
		gzipSizes:   []int{128, 256, 512},
		zstdSizes:   []int{128, 256, 512, 1024, 2048, 4096, 8192},
		brotliSizes: []int{128, 256, 512, 1024, 2048, 4096, 8192},
		last4:       make(map[string][]byte),
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	payloadTypes := []struct {
		sizes      []int
		ext        string
		compressor func(io.Writer, int) error
	}{
		{p.gzipSizes, "gz", gzipCompressor},
		{p.zstdSizes, "zst", zstdCompressor},
		{p.brotliSizes, "br", brotliCompressor},
	}

	for _, pt := range payloadTypes {
		for _, v := range pt.sizes {
			wg.Add(1)
			go func(vv int) {
				defer wg.Done()
				sem <- struct{}{}
				last4, err := generatePayload(vv, pt.ext, pt.compressor)
				if err != nil {
					panic(err)
				}
				p.last4[fmt.Sprintf("%dMB.%s", vv, pt.ext)] = last4
				<-sem
			}(v)
		}
	}

	wg.Wait()
	slog.Info("done generating payloads")
	return p
}

func (p *Payloads) SelectFile(encoding string) string {
	var sizes []int
	switch encoding {
	case "zstd":
		sizes = p.zstdSizes
	case "br":
		sizes = p.brotliSizes
	default:
		sizes = p.gzipSizes
	}
	size := sizes[rand.Intn(len(sizes))]
	return fmt.Sprintf("%dMB.%s", size, extForEncoding(encoding))
}

func (p *Payloads) SelectPayload(encoding string) (trimFn string, last4 []byte) {
	fn := p.SelectFile(encoding)
	return fn + ".trim", p.last4[fn]
}

func extForEncoding(encoding string) string {
	switch encoding {
	case "zstd":
		return "zst"
	case "br":
		return "br"
	default:
		return "gz"
	}
}

func generatePayload(mb int, ext string, c func(io.Writer, int) error) ([]byte, error) {
	fn := fmt.Sprintf("%dMB.%s", mb, ext)
	if _, err := os.Open(fn); err == nil {
		return extractLast4(fn)
	}
	f, err := os.Create(fn)
	if err != nil {
		return nil, err
	}
	if err := c(f, mb); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return extractLast4(fn)
}

func extractLast4(fn string) ([]byte, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	last4 := make([]byte, 4)
	_, err = f.ReadAt(last4, fi.Size()-4)
	if err != nil {
		return nil, err
	}

	trimFn := fn + ".trim"
	trim, err := os.Create(trimFn)
	if err != nil {
		return nil, err
	}
	defer trim.Close()

	_, err = io.CopyN(trim, f, fi.Size()-4)
	if err != nil {
		return nil, err
	}

	slog.Info("generated payload", "file", fn, "bytes", fi.Size())
	return last4, nil
}

func gzipCompressor(w io.Writer, mb int) error {
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer gz.Close()
	fillPayload(gz, mb)
	return gz.Flush()
}

func zstdCompressor(w io.Writer, mb int) error {
	zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return err
	}
	defer zw.Close()
	fillPayload(zw, mb)
	return nil
}

func brotliCompressor(w io.Writer, mb int) error {
	bw := brotli.NewWriterLevel(w, brotli.BestCompression)
	defer bw.Close()
	fillPayload(bw, mb)
	return nil
}

func fillPayload(w io.Writer, mb int) {
	trashWord, trashBuf := []byte("&#x1f33d;"), []byte("<html><head><title>")
	for len(trashBuf) < 4096 {
		trashBuf = append(trashBuf, trashWord...)
	}
	for i := 0; i < (mb*1024*1024)/4096; i++ {
		w.Write(trashBuf)
	}
	w.Write([]byte("</title></head><body><a href=\"http://corn.cash:8080/BOTS\">corn</a></body></html>"))
}
