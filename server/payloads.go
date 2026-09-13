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
}

func MakePayloads() *Payloads {
	p := &Payloads{
		gzipSizes:   []int{128, 256, 512},
		zstdSizes:   []int{128, 256, 512, 1024, 2048, 4096, 8192},
		brotliSizes: []int{128, 256, 512, 1024, 2048, 4096, 8192},
	}

	var wg sync.WaitGroup
	for _, v := range p.gzipSizes {
		wg.Add(1)
		go func(vv int) {
			defer wg.Done()
			if err := generatePayload(vv, "gz", gzipCompressor); err != nil {
				panic(err)
			}
		}(v)
	}
	for _, v := range p.zstdSizes {
		wg.Add(1)
		go func(vv int) {
			defer wg.Done()
			if err := generatePayload(vv, "zst", zstdCompressor); err != nil {
				panic(err)
			}
		}(v)
	}
	for _, v := range p.brotliSizes {
		wg.Add(1)
		go func(vv int) {
			defer wg.Done()
			if err := generatePayload(vv, "br", brotliCompressor); err != nil {
				panic(err)
			}
		}(v)
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

func generatePayload(mb int, ext string, c func(io.Writer, int) error) error {
	fn := fmt.Sprintf("%dMB.%s", mb, ext)
	if f, err := os.Open(fn); err == nil {
		f.Close()
		return nil
	}
	f, err := os.Create(fn)
	if err != nil {
		return err
	}
	if err := c(f, mb); err != nil {
		f.Close()
		return err
	}
	fi, err := f.Stat()
	f.Close()
	if err != nil {
		return err
	}
	slog.Info("generated payload", "file", fn, "bytes", fi.Size())
	return nil
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
