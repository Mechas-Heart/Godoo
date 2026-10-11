// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package version holds build metadata, set at build time via -ldflags.
package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
