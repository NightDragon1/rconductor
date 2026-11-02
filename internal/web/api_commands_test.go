// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/commands"
)

// TestCommandErrors: refusals map to Design 3.5; a rate limit answers 429
// with the wait (CMD-019).
func TestCommandErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		reason api.ReasonCode
		retry  int
	}{
		"not in list":  {&commands.DeniedError{Reason: commands.ReasonNotInList}, http.StatusForbidden, api.ReasonCodeNotInList, 0},
		"wrapper":      {&commands.DeniedError{Reason: commands.ReasonWrapperBlocked}, http.StatusForbidden, api.ReasonCodeWrapperBlocked, 0},
		"invalid":      {&commands.DeniedError{Reason: commands.ReasonInvalid}, http.StatusUnprocessableEntity, api.ReasonCodeInvalid, 0},
		"rate limited": {&commands.DeniedError{Reason: commands.ReasonRateLimited, RetryAfter: 2100 * time.Millisecond}, http.StatusTooManyRequests, api.ReasonCodeRateLimited, 3},
		"unavailable":  {commands.ErrUnavailable, http.StatusServiceUnavailable, api.ReasonCodeServerUnavailable, 0},
		"timeout":      {commands.ErrTimeout, http.StatusGatewayTimeout, "", 0},
	} {
		var p *problem
		if !errors.As(commandError(tc.err), &p) || p.status != tc.status || p.reason != tc.reason || p.retryAfter != tc.retry {
			t.Errorf("%s: %+v", name, p)
		}
	}
}
