// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// progressiveJPEG is a valid progressive JPEG, tiny as a file and costly to
// decode. acScans adds scans of few bytes each.
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

// TestR5B11CMYKJPEGIsRefused: a progressive JPEG with four components is
// refused, its decoding would take far more than its weight.
func TestR5B11CMYKJPEGIsRefused(t *testing.T) {
	_, _, err := processAvatar(context.Background(), progressiveJPEG(4096, 4, 0))
	var invalid *InvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("CMYK picture = %v", err)
	}
}

// TestR5B11ProgressiveJPEGStaysWithinItsWeight: the worst picture still allowed
// takes no more than the budget says.
func TestR5B11ProgressiveJPEGStaysWithinItsWeight(t *testing.T) {
	data := progressiveJPEG(maxAvatarPixels, 3, 0)
	runtime.GC()
	var start runtime.MemStats
	runtime.ReadMemStats(&start)
	var peak atomic.Uint64
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
		}
	}()
	_, _, err := processAvatar(context.Background(), data)
	close(stop)
	if err != nil {
		t.Fatal(err)
	}
	used := peak.Load() - min(peak.Load(), start.HeapAlloc)
	t.Logf("peak %d MiB", used>>20)
	if weight := uint64(maxAvatarPixels * maxAvatarPixels * avatarBytesPerPixel); used > weight {
		t.Errorf("decoding took %d MiB, its weight is %d MiB", used>>20, weight>>20)
	}
}

// TestR5B13ScanBomb: a picture with too many scans is refused before it is
// decoded (#161).
func TestR5B13ScanBomb(t *testing.T) {
	data := progressiveJPEG(maxAvatarPixels, 3, 26000)
	began := time.Now()
	_, _, err := processAvatar(context.Background(), data)
	var invalid *InvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(began); d > 10*time.Second {
		t.Errorf("decoding took %v", d)
	}
}
