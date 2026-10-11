// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Package ui prints consistent, human-readable progress messages. Progress
// goes to stderr so stdout stays clean for data (godoo db list, godoo env).
package ui

import (
	"fmt"
	"os"
)

// Verbose enables Debug output (set by the global -v flag).
var Verbose bool

// Step announces a top-level action: "==> building nix environment".
func Step(format string, a ...any) { fmt.Fprintf(os.Stderr, "==> "+format+"\n", a...) }

// Info prints an indented detail line under a step.
func Info(format string, a ...any) { fmt.Fprintf(os.Stderr, "    "+format+"\n", a...) }

// Warn prints to stderr so it is visible even when stdout is piped.
func Warn(format string, a ...any) { fmt.Fprintf(os.Stderr, "warning: "+format+"\n", a...) }

// Debug prints only with -v; used to show the exact commands being run.
func Debug(format string, a ...any) {
	if Verbose {
		fmt.Fprintf(os.Stderr, "+ "+format+"\n", a...)
	}
}
