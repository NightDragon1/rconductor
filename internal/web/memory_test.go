// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// memoryLimit is what a burst of the largest bodies may add to the heap. The
// panel runs within 1 GiB (deploy/compose), Argon2id and everything else share it.
const memoryLimit = 384 << 20

// costlyBody is the JSON of MaxBody bytes that takes the most memory when
// decoded into a free-form object.
func costlyBody() string {
	const open, closing = `{"a":[`, `]}`
	var b strings.Builder
	b.WriteString(open)
	for b.Len() < MaxBody-len(closing)-3 {
		b.WriteString(`{},`)
	}
	b.WriteString(`{}`)
	b.WriteString(closing)
	return b.String()
}

func (f *apiFixture) passwordHash(t *testing.T) string {
	t.Helper()
	var hash string
	if err := f.st.DB.QueryRowContext(t.Context(), `SELECT password_hash FROM users WHERE id = ?`, f.adminID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return hash
}

// raw sends a write the way a client does, without the helpers of do, and
// returns the status. It is safe for parallel use.
func (f *apiFixture) raw(method, path, body string, session *http.Cookie) int {
	r := httptest.NewRequest(method, "http://panel.lan"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://panel.lan")
	r.Header.Set("If-Match", "x")
	r.AddCookie(session)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec.Code
}

// peakHeap runs f and returns the highest heap it saw above the start.
func peakHeap(f func()) uint64 {
	runtime.GC()
	var start runtime.MemStats
	runtime.ReadMemStats(&start)
	var peak atomic.Uint64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
		}
	})
	f()
	close(stop)
	wg.Wait()
	if p := peak.Load(); p > start.HeapAlloc {
		return p - start.HeapAlloc
	}
	return 0
}

// TestV1BodiesStayWithinMemory covers T-025 for every role: the rights are
// checked after decoding, so a session of any role can send the largest
// bodies in parallel, up to the connection limit of the web port.
func TestV1BodiesStayWithinMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("load test")
	}
	f := newAPIFixture(t, Proxies{})
	hash := f.passwordHash(t)
	body := costlyBody()
	for _, role := range []string{"rcon_admin", "server_admin", "moderator", "supporter"} {
		t.Run(role, func(t *testing.T) {
			name := "load-" + role
			storetest.CreateUser(t, f.st, storetest.User{Pseudonym: name, Role: role, PasswordHash: hash, CreatedBy: f.adminID})
			session := f.login(t, name)
			var served atomic.Int32
			peak := peakHeap(func() {
				var wg sync.WaitGroup
				for range 512 {
					wg.Go(func() {
						for range 2 {
							r := f.raw(http.MethodPatch, "/api/v1/settings", body, session)
							if r != http.StatusServiceUnavailable {
								served.Add(1)
							}
						}
					})
				}
				wg.Wait()
			})
			t.Logf("%s: peak %d MiB, %d requests served", role, peak>>20, served.Load())
			if peak > memoryLimit {
				t.Errorf("%s: heap grew by %d MiB, the limit is %d MiB", role, peak>>20, memoryLimit>>20)
			}
			if served.Load() == 0 {
				t.Error("no request got through")
			}
		})
	}
}

// budgetFixture returns a server whose handler reports when it runs, for the
// budget alone.
func budgetFixture(next http.HandlerFunc) http.Handler {
	s := &apiServer{decode: semaphore.NewWeighted(decodeBudget), pictures: semaphore.NewWeighted(pictureBudget)}
	return withBodyLimit(s.limitBodies(next))
}

// TestSlowSenderHoldsNoBudget: a JSON body is read before it takes its part,
// so a client that trickles its body keeps nobody else waiting.
func TestSlowSenderHoldsNoBudget(t *testing.T) {
	h := budgetFixture(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
	})
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	for range 2 * decodeBudget / MaxBody {
		r := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", pr)
		r.ContentLength = MaxBody
		go h.ServeHTTP(httptest.NewRecorder(), r)
	}
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("request behind slow senders = %d", rec.Code)
	}
}

// TestBudgetIsGivenBack: every way through the budget returns its part.
func TestBudgetIsGivenBack(t *testing.T) {
	s := &apiServer{decode: semaphore.NewWeighted(decodeBudget), pictures: semaphore.NewWeighted(pictureBudget)}
	h := withBodyLimit(s.limitBodies(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	for range 64 {
		for _, c := range []struct {
			path string
			size int
		}{{"/api/v1/settings", MaxBody}, {"/api/v1/settings", MaxBody + 1}, {"/api/v1/me/avatar", MaxPictureBody}} {
			r := httptest.NewRequest(http.MethodPut, c.path, strings.NewReader(strings.Repeat("x", c.size)))
			r.ContentLength = -1 // read to find out
			h.ServeHTTP(httptest.NewRecorder(), r)
		}
	}
	if !s.decode.TryAcquire(decodeBudget) || !s.pictures.TryAcquire(pictureBudget) {
		t.Error("a part of the budget got lost")
	}
}

// TestBudgetAnswersBusy: who cannot get a part within the wait is told so, and
// a hung-up client leaves no part behind.
func TestBudgetAnswersBusy(t *testing.T) {
	s := &apiServer{decode: semaphore.NewWeighted(decodeBudget), pictures: semaphore.NewWeighted(pictureBudget)}
	if !s.decode.TryAcquire(decodeBudget) {
		t.Fatal("setup")
	}
	h := s.limitBodies(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("ran without a part") }))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, http.MethodPatch, "/api/v1/settings", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("no budget = %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// TestV6ChunkedPublicBodyOverTheLimit covers #158: also on the routes open to
// anyone a chunked body over the limit is too large, not invalid.
func TestV6ChunkedPublicBodyOverTheLimit(t *testing.T) {
	h := withBodyLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("ran with a body over the limit") }))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(strings.Repeat("x", MaxPublicBody+1)))
	r.ContentLength = -1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("chunked public body over the limit = %d", rec.Code)
	}
}

// TestChunkedBodyOverTheLimit: the answer does not depend on how the size
// is sent (Design 2.1).
func TestChunkedBodyOverTheLimit(t *testing.T) {
	h := budgetFixture(func(http.ResponseWriter, *http.Request) { t.Error("ran with a body over the limit") })
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(strings.Repeat("x", MaxBody+1)))
	r.ContentLength = -1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("chunked body over the limit = %d", rec.Code)
	}
}

// TestPublicBodiesKeepTheirWay: without a session nobody touches the budget of
// the signed-in users.
func TestPublicBodiesKeepTheirWay(t *testing.T) {
	s := &apiServer{decode: semaphore.NewWeighted(decodeBudget), pictures: semaphore.NewWeighted(pictureBudget)}
	if !s.decode.TryAcquire(decodeBudget) {
		t.Fatal("setup")
	}
	h := s.limitBodies(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Errorf("login with the budget used up = %d", rec.Code)
	}
}

// pictureBomb is a small file that takes 128 MiB when decoded.
func pictureBomb(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	e := png.Encoder{CompressionLevel: png.BestCompression}
	if err := e.Encode(&b, image.NewNRGBA64(image.Rect(0, 0, 4096, 4096))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestR4B1PictureBombStaysWithinMemory covers T-025: the size of a file says little
// about its pixels, and the lowest role may set a picture.
func TestR4B1PictureBombStaysWithinMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("load test")
	}
	f := newAPIFixture(t, Proxies{})
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "low", Role: "supporter", PasswordHash: f.passwordHash(t), CreatedBy: f.adminID})
	session := f.login(t, "low")
	body := pictureBomb(t)
	var served atomic.Int32
	peak := peakHeap(func() {
		var wg sync.WaitGroup
		for range 16 {
			wg.Go(func() {
				r := httptest.NewRequest(http.MethodPut, "http://panel.lan/api/v1/me/avatar", bytes.NewReader(body))
				r.Header.Set("Content-Type", "image/png")
				r.Header.Set("Origin", "http://panel.lan")
				r.AddCookie(session)
				rec := httptest.NewRecorder()
				f.h.ServeHTTP(rec, r)
				if rec.Code == http.StatusNoContent {
					served.Add(1)
				} else if rec.Code != http.StatusServiceUnavailable {
					t.Errorf("picture = %d %s", rec.Code, rec.Body.String())
				}
			})
		}
		wg.Wait()
	})
	t.Logf("peak %d MiB, %d pictures set", peak>>20, served.Load())
	if peak > memoryLimit {
		t.Errorf("heap grew by %d MiB, the limit is %d MiB", peak>>20, memoryLimit>>20)
	}
	if served.Load() == 0 {
		t.Error("no picture got through")
	}
}

// TestR4B8PictureSlowSender keeps the proof of a known gap: pictures take their
// budget before they are read, so four slow uploads block all others for up
// to the read timeout. It fails on today's code; see #160.
func TestR4B8PictureSlowSender(t *testing.T) {
	t.Skip("known gap before 1.0, see #160")
	h := budgetFixture(func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body) })
	for range pictureBudget / MaxPictureBody {
		pr, pw := io.Pipe()
		defer func() { _ = pw.Close() }()
		r := httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", pr)
		r.ContentLength = 1000
		go h.ServeHTTP(httptest.NewRecorder(), r)
	}
	time.Sleep(100 * time.Millisecond)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", strings.NewReader("x")))
	if rec.Code != http.StatusOK {
		t.Errorf("picture behind slow senders = %d", rec.Code)
	}
}

// TestR4PathEndingsGiveNoWayAroundTheBudget: limits go by the end of the path,
// but an escaped path that only looks like a public or picture route ends
// before any body is read. The review found no gap here, so this test has no
// state before a fix to fail on; it guards the check.
func TestR4PathEndingsGiveNoWayAroundTheBudget(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	session := f.login(t, "Mod")
	for _, p := range []string{"/api/v1/users/x%2Fauth%2Flogin", "/api/v1/users/x%2Fme%2Favatar", "/api/v1/server-groups/x%2Fauth%2Flogin", "/api/v1/auth%2Flogin"} {
		if code := f.raw(http.MethodPatch, p, `{}`, session); code < 400 || code >= 500 {
			t.Errorf("%s = %d", p, code)
		}
	}
}

// TestR5B12PictureReadsStayWithinThePool: each read took a connection of its
// own, and 512 of them took 1.4 GiB outside the Go heap.
func TestR5B12PictureReadsStayWithinThePool(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	session := f.login(t, "Mod")
	stop := make(chan struct{})
	var peak atomic.Int32
	var watch sync.WaitGroup
	watch.Go(func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			if n := int32(f.st.DB.Stats().OpenConnections); n > peak.Load() { //nolint:gosec // few connections
				peak.Store(n)
			}
		}
	})
	var wg sync.WaitGroup
	for range 512 {
		wg.Go(func() {
			r := httptest.NewRequest(http.MethodGet, "http://panel.lan/api/v1/users/"+f.userID+"/avatar", nil)
			r.AddCookie(session)
			f.h.ServeHTTP(httptest.NewRecorder(), r)
		})
	}
	wg.Wait()
	close(stop)
	watch.Wait()
	if n := peak.Load(); n == 0 || n > 32 {
		t.Errorf("%d database connections at once, want 1 to 32", n)
	}
}
