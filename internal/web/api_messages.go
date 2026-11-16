// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"

	"github.com/NightDragon1/rconductor/internal/api"
)

// Messages in the panel (MSG-001 to MSG-003, D-085, D-086): contract
// first; the handlers answer 501 behind the session and the flag messages
// until the backend is built.

func (s *apiServer) ListMessages(context.Context, api.ListMessagesRequestObject) (api.ListMessagesResponseObject, error) {
	return nil, notImplemented("listMessages")
}

func (s *apiServer) CreateMessage(context.Context, api.CreateMessageRequestObject) (api.CreateMessageResponseObject, error) {
	return nil, notImplemented("createMessage")
}

func (s *apiServer) GetMessage(context.Context, api.GetMessageRequestObject) (api.GetMessageResponseObject, error) {
	return nil, notImplemented("getMessage")
}

func (s *apiServer) DeleteMessage(context.Context, api.DeleteMessageRequestObject) (api.DeleteMessageResponseObject, error) {
	return nil, notImplemented("deleteMessage")
}

func (s *apiServer) MarkMessageRead(context.Context, api.MarkMessageReadRequestObject) (api.MarkMessageReadResponseObject, error) {
	return nil, notImplemented("markMessageRead")
}
