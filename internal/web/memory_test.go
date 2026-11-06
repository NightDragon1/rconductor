// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
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

// TestBodiesStayWithinMemory covers T-025 for every role: the rights are
// checked after decoding, so a session of any role can send the largest
// bodies in parallel, up to the connection limit of the web port.
func TestBodiesStayWithinMemory(t *testing.T) {
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
