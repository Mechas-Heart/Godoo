// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package version

import "testing"

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v0.7.1":              true,
		"v10.0.12":            true,
		"0.7.1":               false, // Normalize first
		"dev":                 false,
		"v0.7.1-3-gabc-dirty": false,
		"v0.7.1+dirty":        false,
		"v0.7.1-dirty":        false,
	} {
		if IsRelease(v) != want {
			t.Errorf("IsRelease(%q) = %v", v, !want)
		}
	}
	if Normalize("0.7.1") != "v0.7.1" || Normalize("v0.7.1") != "v0.7.1" || Normalize("") != "" {
		t.Error("Normalize")
	}
}

func TestCompatible(t *testing.T) {
	for _, c := range []struct {
		running, pinned string
		want            bool
	}{
		{"v0.8.0", "v0.8.0", true},
		{"v0.8.3", "v0.8.0", true},  // bug fixes on top
		{"v0.8.0", "v0.8.2", false}, // misses fixes the project has
		{"v0.9.0", "v0.8.2", false}, // new minor: generated files may differ
		{"v0.7.9", "v0.8.0", false},
		{"v1.8.0", "v0.8.0", false},
	} {
		if got := Compatible(c.running, c.pinned); got != c.want {
			t.Errorf("Compatible(%s, %s) = %v, want %v", c.running, c.pinned, got, c.want)
		}
	}
}
