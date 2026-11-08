// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
)

// notImplemented is the answer of a stub handler while an API change is
// built contract first: the operation is in api/openapi.yaml and behind
// the usual session and rights checks, but not done yet. The gate review
// needs none left (make stubs, TestNotImplementedHandlers).
func notImplemented(operationID string) error {
	return &problem{status: http.StatusNotImplemented, reason: api.ReasonCodeInvalid, detail: operationID + " is not implemented yet"}
}
