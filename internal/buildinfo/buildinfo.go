// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package buildinfo holds release information set at build time.
package buildinfo

import (
	"strconv"
	"strings"
)

// Name, license, author and repository of the project (AGT-016, NFR-021).
const (
	Name      = "RCONductor"
	License   = "AGPL-3.0-or-later"
	Copyright = "Copyright (C) 2026 NightDragon · ShadowGhost.NET"
	RepoURL   = "https://github.com/NightDragon1/rconductor"
)

// Unknown is the value of Commit and BuildDate in a build without ldflags.
const Unknown = "unknown"

// Version is the release version, set via
// -ldflags "-X github.com/NightDragon1/rconductor/internal/buildinfo.Version=1.0".
// Versions have two parts, the first is the major version (D-047).
var Version = "0.0.0-dev"

// Commit is the Git commit of the build, set via -ldflags "-X .../buildinfo.Commit=<sha>".
var Commit = Unknown

// BuildDate is the build time in UTC as RFC 3339, set via
// -ldflags "-X .../buildinfo.BuildDate=2026-11-15T08:00:00Z".
var BuildDate = Unknown

// Banner returns the lines that panel and agent log at start and that
// "rconductor-agent version" prints (AGT-016); component is "Panel" or "Agent".
func Banner(component string) []string {
	return []string{
		Name + " " + component + " " + Version + " (" + Commit + ", " + BuildDate + ")",
		Copyright,
		"Licensed under " + License + ". This program comes with ABSOLUTELY NO WARRANTY.",
		"For more see " + RepoURL,
	}
}

// Major returns the major version number; panel and agent must match on it (D-023).
// It returns -1 if Version does not start with a number.
func Major() int { return MajorOf(Version) }

// MajorOf returns the major version number of v, or -1 if v does not start
// with a number.
func MajorOf(v string) int {
	v = strings.TrimPrefix(v, "v")
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	if err != nil || n < 0 {
		return -1
	}
	return n
}
