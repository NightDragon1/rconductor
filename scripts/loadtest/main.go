// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// Loadtest measures a panel on this machine for the residual risks of
// Sicherheit chapter 5; scripts/measure-panel.sh starts it. It sends from many
// loopback addresses, so it works on Linux only and against 127.0.0.1 only.
//
//	-m body     R-07: 640 workers send the costliest JSON of 256 KiB
//	-m picture  R-07: 64 workers upload the costliest picture allowed
//	-m read     R-07: 640 workers read the picture at -p, a database connection each
//	-m all      R-07: bodies, pictures, reads and password checks at once
//	-m hold     R-06: 4 addresses hold 128 connections each and reconnect at once
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
	target  = flag.String("t", "127.0.0.1:18080", "address of the panel")
	mode    = flag.String("m", "body", "body, picture, read, all or hold")
	cookie  = flag.String("c", "", "session cookie")
	picture = flag.String("p", "", "path of a picture for -m read and all")
	dur     = flag.Duration("d", 120*time.Second, "duration")
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

func request(method, path, kind string, body []byte) []byte {
	head := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\nOrigin: http://%s\r\nCookie: %s\r\nContent-Type: %s\r\nIf-Match: x\r\nContent-Length: %d\r\n\r\n", method, path, *target, *target, *cookie, kind, len(body))
	return append([]byte(head), body...)
}

func patchSettings() []byte {
	return request(http.MethodPatch, "/api/v1/settings", "application/json", costly())
}

// putPicture is the costliest picture allowed: a small file that takes 434 MiB
// when decoded.
func putPicture() []byte {
	return request(http.MethodPut, "/api/v1/me/avatar", "image/jpeg", progressiveJPEG(4096, 3, 0))
}

// progressiveJPEG is a valid progressive JPEG from the blind review (round 5):
// tiny as a file, costly to decode. acScans adds scans of few bytes each.
func progressiveJPEG(n, components, acScans int) []byte {
	var b bytes.Buffer
	seg := func(marker byte, p []byte) {
		b.Write([]byte{0xff, marker, byte((len(p) + 2) >> 8), byte(len(p) + 2)})
		b.Write(p)
	}
	b.Write([]byte{0xff, 0xd8})
	seg(0xee, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 0}) // transform 0: CMYK
	q := make([]byte, 65)
	for i := 1; i < 65; i++ {
		q[i] = 1
	}
	seg(0xdb, q)
	sof := []byte{8, byte(n >> 8), byte(n), byte(n >> 8), byte(n), byte(components)}
	for i := range components {
		sof = append(sof, byte(i+1), 0x11, 0)
	}
	seg(0xc2, sof)
	lens := make([]byte, 16)
	lens[0] = 1
	seg(0xc4, append(append([]byte{0x00}, lens...), 0x00)) // DC 0: code "0" = symbol 0
	seg(0xc4, append(append([]byte{0x10}, lens...), 0xe0)) // AC 0: code "0" = EOB run of 2^14+bits
	sos := []byte{byte(components)}
	for i := range components {
		sos = append(sos, byte(i+1), 0)
	}
	sos = append(sos, 0, 0, 0)
	seg(0xda, sos)
	blocks := (n / 8) * (n / 8) * components
	b.Write(make([]byte, (blocks+7)/8))
	perComp := (n / 8) * (n / 8)
	for range acScans {
		seg(0xda, []byte{1, 1, 0, 1, 63, 0})
		// each EOB: bit 0, then 14 extra bits of 1 => run of 32767 blocks.
		var bits []byte
		for left := perComp; left > 0; left -= 32767 {
			bits = append(bits, 0)
			for range 14 {
				bits = append(bits, 1)
			}
		}
		for len(bits)%8 != 0 {
			bits = append(bits, 1)
		}
		for i := 0; i < len(bits); i += 8 {
			var v byte
			for j := range 8 {
				v = v<<1 | bits[i+j]
			}
			b.WriteByte(v)
			if v == 0xff {
				b.WriteByte(0)
			}
		}
	}
	b.Write([]byte{0xff, 0xd9})
	return b.Bytes()
}

// getPicture reads the picture at -p.
func getPicture() []byte {
	return []byte(fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nCookie: %s\r\n\r\n", *picture, *target, *cookie))
}

// signIn costs a password check.
func signIn() []byte {
	return request(http.MethodPost, "/api/v1/auth/login", "application/json", []byte(`{"pseudonym":"nobody","password":"wrong password!!"}`))
}

// flood sends req from workers, each connection from a new address.
func flood(wg *sync.WaitGroup, end time.Time, workers, first int, req []byte, count func(string)) {
	for w := range workers {
		wg.Go(func() {
			n := first + w*100
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
		flood(&wg, end, 640, 0, patchSettings(), count)
	case "picture":
		flood(&wg, end, 64, 0, putPicture(), count)
	case "read":
		flood(&wg, end, 640, 0, getPicture(), count)
	case "all":
		flood(&wg, end, 240, 0, patchSettings(), count)
		flood(&wg, end, 64, 24000, putPicture(), count)
		flood(&wg, end, 240, 31000, getPicture(), count)
		flood(&wg, end, 64, 55000, signIn(), count)
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
