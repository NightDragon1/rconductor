// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// Loadtest measures a panel on this machine for the residual risks of
// Sicherheit chapter 5; scripts/measure-panel.sh starts it. It sends from many
// loopback addresses, so it works on Linux only and against 127.0.0.1 only.
//
//	-m body  R-07: 640 workers send the costliest JSON of 256 KiB
//	-m hold  R-06: 4 addresses hold 128 connections each and reconnect at once
//
// Meanwhile a new visitor comes every 2 s, and the end shows how they fared.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	target = flag.String("t", "127.0.0.1:18080", "address of the panel")
	mode   = flag.String("m", "body", "body or hold")
	cookie = flag.String("c", "", "session cookie for -m body")
	dur    = flag.Duration("d", 120*time.Second, "duration")
)

func dialFrom(i int) (net.Conn, error) {
	src := &net.TCPAddr{IP: net.IPv4(127, 1, byte(i>>8), byte(i&255))}
	d := net.Dialer{LocalAddr: src, Timeout: 10 * time.Second}
	return d.Dial("tcp", *target)
}

func costly() []byte {
	var b bytes.Buffer
	b.WriteString(`{"a":[`)
	for b.Len() < 256<<10-8 {
		b.WriteString(`{},`)
	}
	b.WriteString(`{}]}`)
	return b.Bytes()
}

func main() {
	flag.Parse()
	end := time.Now().Add(*dur)
	var codes sync.Map
	count := func(c string) {
		v, _ := codes.LoadOrStore(c, new(atomic.Int64))
		v.(*atomic.Int64).Add(1)
	}
	var wg sync.WaitGroup
	switch *mode {
	case "body":
		body := costly()
		req := []byte(fmt.Sprintf("PATCH /api/v1/settings HTTP/1.1\r\nHost: %s\r\nOrigin: http://%s\r\nCookie: %s\r\nContent-Type: application/json\r\nIf-Match: x\r\nContent-Length: %d\r\n\r\n", *target, *target, *cookie, len(body)))
		req = append(req, body...)
		for w := range 640 {
			wg.Go(func() {
				n := w * 100
				for time.Now().Before(end) {
					n++
					c, err := dialFrom(n % 60000)
					if err != nil {
						count("dial")
						continue
					}
					_ = c.SetDeadline(time.Now().Add(15 * time.Second))
					r := bufio.NewReader(c)
					for range 3 {
						if _, err := c.Write(req); err != nil {
							count("write")
							break
						}
						resp, err := http.ReadResponse(r, nil)
						if err != nil {
							count("read")
							break
						}
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
						count(fmt.Sprint(resp.StatusCode))
					}
					_ = c.Close()
				}
			})
		}
	case "hold":
		// 4 addresses, 128 connections each, reconnect at once when ended.
		var reconnects atomic.Int64
		for a := range 4 {
			for range 128 {
				wg.Go(func() {
					for time.Now().Before(end) {
						c, err := dialFrom(a + 1)
						if err != nil {
							time.Sleep(100 * time.Millisecond)
							continue
						}
						reconnects.Add(1)
						for time.Now().Before(end) {
							_ = c.SetDeadline(time.Now().Add(30 * time.Second))
							if _, err := fmt.Fprintf(c, "GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
								break
							}
							if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
								break
							}
							time.Sleep(20 * time.Second)
						}
						_ = c.Close()
					}
				})
			}
		}
		defer func() { fmt.Println("connects", reconnects.Load()) }()
	}
	// a new visitor from a fresh address every 2 s
	var lat []time.Duration
	var fails int
	var vis sync.WaitGroup
	var mu sync.Mutex
	for i := 0; time.Now().Before(end); i++ {
		vis.Go(func() {
			t0 := time.Now()
			c, err := dialFrom(60001 + i%5000)
			ok := false
			if err == nil {
				_ = c.SetDeadline(time.Now().Add(60 * time.Second))
				fmt.Fprintf(c, "GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")
				l, err := bufio.NewReader(c).ReadString('\n')
				ok = err == nil && strings.Contains(l, "HTTP/1.1")
				_ = c.Close()
			}
			mu.Lock()
			if ok {
				lat = append(lat, time.Since(t0))
			} else {
				fails++
			}
			mu.Unlock()
		})
		time.Sleep(2 * time.Second)
	}
	wg.Wait()
	vis.Wait()
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	codes.Range(func(k, v any) bool { fmt.Println(k, v.(*atomic.Int64).Load()); return true })
	if len(lat) > 0 {
		fmt.Println("visitor ok", len(lat), "fail", fails, "p50", lat[len(lat)/2].Round(time.Millisecond), "max", lat[len(lat)-1].Round(time.Millisecond))
	} else {
		fmt.Println("visitor ok 0 fail", fails)
	}
}
