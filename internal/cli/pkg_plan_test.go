// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestSplitSpec(t *testing.T) {
	cases := []struct {
		spec, name, version string
		bad                 bool
	}{
		{spec: "PyPDF2", name: "PyPDF2"},
		{spec: "PyPDF2==2.12.1", name: "PyPDF2", version: "2.12.1"},
		{spec: " odoorpc == 0.9.0 ", name: "odoorpc", version: "0.9.0"},
		{spec: "PyPDF2>=2", bad: true},
		{spec: "PyPDF2<3", bad: true},
		{spec: "PyPDF2~=2.12", bad: true},
		{spec: "PyPDF2==", bad: true},
	}
	for _, c := range cases {
		name, version, err := splitSpec(c.spec)
		if c.bad {
			if err == nil {
				t.Errorf("%q: expected an error", c.spec)
			}
			continue
		}
		if err != nil || name != c.name || version != c.version {
			t.Errorf("%q: got %q %q %v, want %q %q", c.spec, name, version, err, c.name, c.version)
		}
	}
}
