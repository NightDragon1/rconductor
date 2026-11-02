// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/api"
)

// hijacked is the answer after the connection became a WebSocket; nothing
// is written any more.
type hijacked struct{}

func (hijacked) VisitOpenWebSocketResponse(http.ResponseWriter) error { return nil }

func (s *apiServer) OpenWebSocket(ctx context.Context, _ api.OpenWebSocketRequestObject) (api.OpenWebSocketResponseObject, error) {
	h := httpOf(ctx)
	// CSRF protection of the upgrade (Design 2.1, 2.3).
	if origin := h.r.Header.Get("Origin"); origin == "" || origin != s.expectedOrigin(h.r) {
		return nil, &problem{status: http.StatusForbidden, reason: api.ReasonCodeForbidden, detail: "origin not allowed"}
	}
	if s.hub == nil {
		return nil, &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeBusy}
	}
	// The server's read and write timeouts must not end the socket.
	rc := http.NewResponseController(h.w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	conn, err := websocket.Accept(h.w, h.r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // origin checked above
	if err != nil {
		return hijacked{}, nil //nolint:nilerr // Accept has written the error answer
	}
	p, _ := principal(ctx)
	s.hub.Serve(ctx, conn, p, RequestID(ctx))
	return hijacked{}, nil
}
