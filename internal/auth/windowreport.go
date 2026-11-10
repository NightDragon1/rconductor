// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"sync"
	"time"
)

// windowReport aggregates a repeated log entry. The first occurrence is
// logged at once and opens a window; the occurrences within it are logged
// as one entry with their number when it ends, and the next window opens.
// A window without any closes, so the next occurrence is logged at once.
type windowReport struct {
	gap time.Duration
	// emit logs one entry for n occurrences; it runs with mu held.
	emit func(n int)

	mu    sync.Mutex
	count int
	// timer is the timer of the running window, nil without one.
	timer *time.Timer
	// gen tells a timer of an ended window from the running one.
	gen int
}

func newWindowReport(gap time.Duration, emit func(n int)) *windowReport {
	return &windowReport{gap: gap, emit: emit}
}

// add counts one occurrence.
func (r *windowReport) add() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	if r.timer != nil {
		return
	}
	r.emitLocked()
	r.gen++
	gen := r.gen
	r.timer = time.AfterFunc(r.gap, func() { r.windowEnded(gen) })
}

// flush logs the occurrences not yet reported and closes the window, so
// that the count stays exact at the shutdown.
func (r *windowReport) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.emitLocked()
}

func (r *windowReport) windowEnded(gen int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer == nil || gen != r.gen {
		return
	}
	if r.count == 0 {
		r.timer = nil
		return
	}
	r.emitLocked()
	r.timer.Reset(r.gap)
}

func (r *windowReport) emitLocked() {
	if r.count == 0 {
		return
	}
	r.emit(r.count)
	r.count = 0
}
