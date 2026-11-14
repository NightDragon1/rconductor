// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web breaks each rule once on purpose.
package web

import "example.invalid/bad/internal/audit"

type apiServer struct{}

func principal() bool { return true }

func (s *apiServer) GetA()  { principal() }
func (s *apiServer) PostB() { principal() } // no audit
func (s *apiServer) GetC()  {}              // no principal
func (s *apiServer) Login() { audit.Write() }
