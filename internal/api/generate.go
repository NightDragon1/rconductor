// SPDX-License-Identifier: AGPL-3.0-or-later

// Package api holds the types and server stubs generated from
// api/openapi.yaml (D-039). Do not edit api.gen.go by hand.
package api

//go:generate go tool -modfile=../../tools/go.mod oapi-codegen -config ../../api/oapi-codegen.yaml -o api.gen.go ../../api/openapi.yaml
