package server

import (
	"bufio"
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

type payloadSpec struct {
	mb   int
	kind string
	ext  string
}

type Payloads struct {
	specs []payloadSpec
	last4 map[string][]byte
}

func MakePayloads() *Payloads {
	p := &Payloads{
		last4: make(map[string][]byte),
	}

	gzipSizes := []int{128, 256, 512}
	zstdSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192}
	brotliSizes := []int{128, 256, 512, 1024, 2048, 4096, 8192}

	payloadConfigs := []struct {
		sizes []int
		ext   string
	}{
		{gzipSizes, "gz"},
		{zstdSizes, "zst"},
		{brotliSizes, "br"},
	}

	kinds := []string{"raw", "json"}

	// Populate specs before spawning goroutines
	for _, kind := range kinds {
		for _, pc := range payloadConfigs {
			for _, v := range pc.sizes {
				p.specs = append(p.specs, payloadSpec{mb: v, kind: kind, ext: pc.ext})
			}
		}
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	var mu sync.Mutex

	// Collect results locally to avoid concurrent map writes
	last4Results := make(map[string][]byte)

	for _, kind := range kinds {
		gen := contentGenerator(kind)
		for _, pc := range payloadConfigs {
			compressor := makeCompressor(pc.ext, gen)
			for _, v := range pc.sizes {
				wg.Add(1)
				go func(vv int) {
					defer wg.Done()
					sem <- struct{}{}
					last4, err := generatePayload(vv, kind, pc.ext, compressor)
					if err != nil {
						panic(err)
					}
					key := fmt.Sprintf("%dMB.%s.%s", vv, kind, pc.ext)
					mu.Lock()
					last4Results[key] = last4
					mu.Unlock()
					<-sem
				}(v)
			}
		}
	}

	wg.Wait()
	p.last4 = last4Results
	slog.Info("done generating payloads")
	return p
}

func contentGenerator(kind string) func(io.Writer, int) error {
	switch kind {
	case "json":
		return GenerateJSON
	default:
		return fillPayload
	}
}

func makeCompressor(ext string, gen func(io.Writer, int) error) func(io.Writer, int) error {
	switch ext {
	case "gz":
		return func(w io.Writer, mb int) error {
			gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
			if err != nil {
				return err
			}
			defer gz.Close()
			if err := gen(gz, mb); err != nil {
				return err
			}
			return gz.Flush()
		}
	case "zst":
		return func(w io.Writer, mb int) error {
			zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
			if err != nil {
				return err
			}
			defer zw.Close()
			if err := gen(zw, mb); err != nil {
				return err
			}
			return nil
		}
	case "br":
		return func(w io.Writer, mb int) error {
			bw := brotli.NewWriterLevel(w, brotli.BestCompression)
			defer bw.Close()
			if err := gen(bw, mb); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

func (p *Payloads) SelectFile(encoding string, kind string) payloadSpec {
	var candidates []payloadSpec
	for _, sp := range p.specs {
		if sp.ext == encoding && sp.kind == kind {
			candidates = append(candidates, sp)
		}
	}
	if len(candidates) == 0 {
		return payloadSpec{}
	}
	return candidates[rand.Intn(len(candidates))]
}

func (p *Payloads) SelectPayload(encoding string, kind string) (trimFn string, last4 []byte) {
	sp := p.SelectFile(encoding, kind)
	fn := fmt.Sprintf("%dMB.%s.%s", sp.mb, sp.kind, sp.ext)
	return fn + ".trim", p.last4[fn]
}

func generatePayload(mb int, kind string, ext string, c func(io.Writer, int) error) ([]byte, error) {
	fn := fmt.Sprintf("%dMB.%s.%s", mb, kind, ext)
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

func fillPayload(w io.Writer, mb int) error {
	trashWord, trashBuf := []byte("&#x1f33d;"), []byte("<html><head><title>")
	for len(trashBuf) < 4096 {
		trashBuf = append(trashBuf, trashWord...)
	}
	for i := 0; i < (mb*1024*1024)/4096; i++ {
		w.Write(trashBuf)
	}
	w.Write([]byte("</title></head><body><a href=\"http://corn.cash:8080/BOTS\">corn</a></body></html>"))
	return nil
}

func GenerateJSON(w io.Writer, mb int) error {
	n := mb * 1024 * 1024 / 8
	if n < 1 {
		n = 1
	}
	prefix := []byte("{\"a\":[")
	core := []byte("{\"a\":[]}")
	suffix := []byte("]}")

	bw := bufio.NewWriter(w)
	for i := 0; i < n-1; i++ {
		bw.Write(prefix)
	}
	bw.Write(core)
	for i := 0; i < n-1; i++ {
		bw.Write(suffix)
	}
	return bw.Flush()
}
