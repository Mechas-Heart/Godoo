// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package version holds build metadata, set at build time via -ldflags
// (make build, make dist). Without ldflags (go install, go build), it falls
// back to what Go records in the binary itself.
package version

import "runtime/debug"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// `go install .../cmd/godoo@v0.2.0` records the module version.
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	// `go build` in a git clone records the commit and its time.
	for _, s := range info.Settings {
		switch {
		case s.Key == "vcs.revision" && Commit == "none" && len(s.Value) >= 7:
			Commit = s.Value[:7]
		case s.Key == "vcs.time" && Date == "unknown":
			Date = s.Value
		}
	}
}
