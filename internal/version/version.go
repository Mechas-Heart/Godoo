// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package version holds build metadata, set at build time via -ldflags
// (make build, make dist). Without ldflags (go install, go build), it falls
// back to what Go records in the binary itself.
package version

import (
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
)

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

var releaseRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Release returns the version when this binary is an exact release
// (v0.7.1), and false for development builds (dev, v0.7.1-3-gabc-dirty,
// v0.7.1+dirty), which have no version to compare a project's pin with.
func Release() (string, bool) {
	return Version, IsRelease(Version)
}

// IsRelease reports whether v is a release version like v0.7.1.
func IsRelease(v string) bool { return releaseRe.MatchString(v) }

// Normalize adds the "v" a version like "0.7.1" lacks.
func Normalize(v string) string {
	if v != "" && v[0] != 'v' {
		return "v" + v
	}
	return v
}

// Compatible reports whether release running can work on a project made with
// release pinned. godoo's version numbers mean:
//
//	v0.8.1 -> v0.8.2   bug fixes only: generated files stay the same
//	v0.8.x -> v0.9.0   new features, or generated files may change
//
// So the same major.minor with an equal or newer patch is compatible; an
// older patch may lack a fix the project relies on.
func Compatible(running, pinned string) bool {
	r, okR := parts(running)
	p, okP := parts(pinned)
	if !okR || !okP {
		return running == pinned
	}
	return r[0] == p[0] && r[1] == p[1] && r[2] >= p[2]
}

// parts splits "v0.8.1" into [0 8 1].
func parts(v string) ([3]int, bool) {
	var out [3]int
	if !IsRelease(v) {
		return out, false
	}
	for i, f := range strings.Split(strings.TrimPrefix(v, "v"), ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
