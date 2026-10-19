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
