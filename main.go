package main

import (
	"bytes"
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
		switch ct := r.Header.Get("content-type"); ct {
		case "image/jpeg", "image/jpg", "image/png":
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
			return fmt.Errorf("goofifying %q: %w", r.Request.URL.String(), err)
		}

		r.Header.Set("content-length", strconv.Itoa(len(out)))
		r.Body = io.NopCloser(bytes.NewReader(out))
		return nil
	}

	return http.ListenAndServe(addr, proxy)
}

func goofify(in []byte) ([]byte, error) {
	if t := bimg.DetermineImageType(in); !bimg.IsTypeSupported(t) {
		log.Printf("Returning image of unsupported type %s unmodified", bimg.ImageTypeName(t))
		return in, nil
	}

	var out []byte
	tf := rand.Intn(len(operations))

	out, err := operations[tf](in)
	if err != nil {
		return nil, fmt.Errorf("rotating image: %w", err)
	}

	return out, nil
}

var operations = []func([]byte) ([]byte, error){
	func(in []byte) ([]byte, error) {
		angle := bimg.Angle(90 * (rand.Intn(2) + 1)) // 90 - 270. Bimg can only rotate in 90deg increments.
		log.Printf("Rotating image by %d degrees", angle)
		return bimg.NewImage(in).Process(bimg.Options{
			Rotate: angle,
		})
	},
	func(in []byte) ([]byte, error) {
		log.Printf("Flopping image")
		return bimg.NewImage(in).Flop()
	},
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
