// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/servers"
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
		"timeout":      {commands.ErrTimeout, http.StatusGatewayTimeout, api.ReasonCodeTimeout, 0},
		"queue full":   {commands.ErrBusy, http.StatusServiceUnavailable, api.ReasonCodeBusy, 1},
	} {
		var p *problem
		if !errors.As(commandError(tc.err), &p) || p.status != tc.status || p.reason != tc.reason || p.retryAfter != tc.retry {
			t.Errorf("%s: %+v", name, p)
		}
	}
}

// TestActionErrors: action failures map to Design 3.5 with the codes of
// revision 2.3 candidate 6.
func TestActionErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		reason api.ReasonCode
	}{
		"no agent":     {servers.ErrAgentUnavailable, http.StatusServiceUnavailable, api.ReasonCodeAgentUnavailable},
		"no rcon":      {servers.ErrRCONUnavailable, http.StatusServiceUnavailable, api.ReasonCodeServerUnavailable},
		"timeout":      {servers.ErrActionTimeout, http.StatusGatewayTimeout, api.ReasonCodeTimeout},
		"rate limited": {servers.ErrActionRateLimited, http.StatusTooManyRequests, api.ReasonCodeRateLimited},
		"refused":      {&servers.ActionFailedError{Code: "forbidden"}, http.StatusBadGateway, api.ReasonCodeActionFailed},
	} {
		var p *problem
		if !errors.As(actionError(tc.err), &p) || p.status != tc.status || p.reason != tc.reason {
			t.Errorf("%s: %+v", name, p)
		}
	}
}

// TestCommandResult covers D-073: a single command answers with result ok,
// or error with reason command_failed and the game's text in response.
func TestCommandResult(t *testing.T) {
	ok := commandResult(commands.Result{RequestID: "0b6f1a7e-8d2c-4f3a-9e1b-2c3d4e5f6a7b", Command: "say hi", Response: "hi", Result: commands.ResultOK})
	if ok.Result != api.CommandResultResultOk || ok.Reason != nil || ok.Response != "hi" {
		t.Errorf("ok: %+v", ok)
	}
	if empty := commandResult(commands.Result{Command: "list"}); empty.Result != api.CommandResultResultOk {
		t.Errorf("without result: %+v", empty)
	}
	failed := commandResult(commands.Result{Command: "nope", Response: "Unknown Console Command", Result: commands.ResultError, Reason: commands.ReasonCommandFailed})
	if failed.Result != api.CommandResultResultError || failed.Reason == nil || *failed.Reason != api.ReasonCodeCommandFailed || failed.Response != "Unknown Console Command" {
		t.Errorf("failed: %+v", failed)
	}
}
