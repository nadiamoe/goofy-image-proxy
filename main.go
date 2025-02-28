package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/h2non/bimg"
)

func main() {
	err := run()
	if err != nil {
		log.Fatalf("%v", err)
	}
}

func run() error {
	baseStr := os.Getenv("UPSTREAM_URL")
	if baseStr == "" {
		return errors.New("UPSTREAM_URL must be set")
	}

	base, err := url.Parse(baseStr)
	if err != nil {
		return fmt.Errorf("parsing UPSTREAM_URL: %w", err)
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	sem := make(semaphore, 4)

	proxy := &httputil.ReverseProxy{}
	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.SetURL(base)
		pr.Out.Header.Set("Host", base.Host)
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		start := time.Now()

		switch ct := r.Header.Get("content-type"); ct {
		case "image/jpeg", "image/jpg", "image/png", "image/webp", "application/octet-stream":
		default:
			log.Printf("Ignoring response with content-type %q", ct)
			return nil
		}

		taken, release := sem.Take(2 * time.Second)
		defer release()

		if !taken {
			log.Println("Timeout waiting for semaphore, returning request unmodified")
			return nil
		}

		cl, err := strconv.Atoi(r.Header.Get("content-length"))
		if err != nil {
			cl = 0
		}

		in := bytes.NewBuffer(make([]byte, 0, cl))
		_, err = in.ReadFrom(r.Body)
		if err != nil {
			return fmt.Errorf("reading image body: %w", err)
		}

		out, err := goofify(in.Bytes())
		if err != nil {
			log.Printf("Error goofifying %q, returning unmodified: %v", r.Request.URL.String(), err)
			r.Body = io.NopCloser(bytes.NewBuffer(in.Bytes()))
			return nil
		}

		log.Printf("Goofified %s in %s", r.Request.URL.Path, time.Since(start).String())
		r.Header.Set("X-Goofy", "1")
		r.Header.Set("content-length", strconv.Itoa(len(out)))
		r.Body = io.NopCloser(bytes.NewReader(out))
		return nil
	}

	log.Printf("Starting HTTP server on %q", addr)
	return http.ListenAndServe(addr, proxy)
}

func goofify(in []byte) ([]byte, error) {
	imageType := bimg.DetermineImageType(in)
	if !bimg.IsTypeSupported(imageType) {
		return nil, fmt.Errorf("unsupported image type %s", bimg.ImageTypeName(imageType))
	}

	localOps := make([]operation, len(operations))
	copy(localOps, operations)

	if imageType == bimg.JPEG {
		localOps = append(localOps, deepFry)
	}

	out, err := localOps[rand.Intn(len(localOps))](in)
	if err != nil {
		return nil, fmt.Errorf("goofifying image: %w", err)
	}

	return out, nil
}

type operation func([]byte) ([]byte, error)

func rotateOp(angle int) operation {
	return func(in []byte) ([]byte, error) {
		log.Printf("Rotating image by %d degrees", angle)
		return bimg.NewImage(in).Rotate(bimg.Angle(angle))
	}
}

func deepFry(in []byte) ([]byte, error) {
	log.Printf("Deep frying image")
	return bimg.NewImage(in).Process(bimg.Options{
		Sharpen: bimg.Sharpen{
			Radius: 3,
			X1:     1.5,
			Y2:     100,
			Y3:     100,
			M1:     5,
			M2:     10,
		},
		Quality: 5,
	})
}

var operations = []operation{
	func(in []byte) ([]byte, error) {
		log.Printf("Blurring image")
		return bimg.NewImage(in).Process(bimg.Options{
			GaussianBlur: bimg.GaussianBlur{
				Sigma: 2,
			},
		})
	},
	func(in []byte) ([]byte, error) {
		log.Printf("Flopping image")
		return bimg.NewImage(in).Flop()
	},
	rotateOp(90),
	rotateOp(180),
	rotateOp(270),
}

type semaphore chan struct{}

func (s semaphore) Take(timeout time.Duration) (bool, func()) {
	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case s <- struct{}{}:
		return true, func() { <-s }
	case <-t.C:
		return false, func() {}
	}
}
