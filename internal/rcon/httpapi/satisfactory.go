// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/rcon"
)

// Satisfactory is the HTTPS API of the Satisfactory dedicated server: one
// path, the function in the body, a bearer token made in the server
// console (D-071).
type Satisfactory struct{}

const satisfactoryPath = "/api/v1"

// Name is the dialect's name in game profiles.
func (Satisfactory) Name() string { return "satisfactory" }

type sfRequest struct {
	Function string `json:"function"`
	Data     any    `json:"data,omitempty"`
}

// sfError is the error body of the API.
type sfError struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

// call sends one function and decodes the data of a 200 answer into out;
// 204 is success without data.
func (Satisfactory) call(ctx context.Context, c Client, function string, data, out any) error {
	body, err := json.Marshal(sfRequest{Function: function, Data: data})
	if err != nil {
		return err
	}
	resp, err := c.Do(ctx, Request{Path: satisfactoryPath, Body: body})
	if err != nil {
		return err
	}
	switch resp.Status {
	case http.StatusNoContent:
		return nil
	case http.StatusOK:
	default:
		return fmt.Errorf("satisfactory %s: HTTP %d", function, resp.Status)
	}
	var e sfError
	if json.Unmarshal(resp.Body, &e) == nil && e.ErrorCode != "" {
		// The code is foreign data; it goes into the error only as a
		// short, quoted value.
		return fmt.Errorf("satisfactory %s: error %.64q", function, e.ErrorCode)
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		return fmt.Errorf("satisfactory %s: %w", function, err)
	}
	if len(env.Data) == 0 {
		return fmt.Errorf("satisfactory %s: no data", function)
	}
	return json.Unmarshal(env.Data, out)
}

// Health asks for the server state, which needs the token, so that a
// wrong token shows at once.
func (d Satisfactory) Health(ctx context.Context, c Client) error {
	var state struct {
		ServerGameState json.RawMessage `json:"serverGameState"`
	}
	return d.call(ctx, c, "QueryServerState", nil, &state)
}

// Execute runs a console command; 204 means it ran without output. A
// returnValue of false is the game's "failed": the text comes back with
// rcon.ErrCommandFailed (D-072).
func (d Satisfactory) Execute(ctx context.Context, c Client, command string) (string, error) {
	var out struct {
		CommandResult string `json:"commandResult"`
		ReturnValue   *bool  `json:"returnValue"`
	}
	if err := d.call(ctx, c, "RunCommand", map[string]string{"command": command}, &out); err != nil {
		return "", err
	}
	if out.ReturnValue != nil && !*out.ReturnValue {
		return out.CommandResult, rcon.ErrCommandFailed
	}
	return out.CommandResult, nil
}

// Players gives the number of players and the limit; the API has no
// names.
func (d Satisfactory) Players(ctx context.Context, c Client) (Players, error) {
	var state struct {
		ServerGameState struct {
			NumConnectedPlayers int `json:"numConnectedPlayers"`
			PlayerLimit         int `json:"playerLimit"`
		} `json:"serverGameState"`
	}
	if err := d.call(ctx, c, "QueryServerState", nil, &state); err != nil {
		return Players{}, err
	}
	return Players{Count: state.ServerGameState.NumConnectedPlayers, Max: state.ServerGameState.PlayerLimit}, nil
}
