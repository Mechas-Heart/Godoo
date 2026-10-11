// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"reflect"
	"testing"
)

func TestCandidates(t *testing.T) {
	tests := map[string][]string{
		"pandas":          {"pandas", "python-pandas"},
		"PyYAML":          {"PyYAML", "pyyaml", "python-pyyaml"},
		"python_dateutil": {"python_dateutil", "python-dateutil", "dateutil"},
		"zope.interface":  {"zope.interface", "zope-interface", "python-zope-interface"},
	}
	for in, want := range tests {
		if got := candidates(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v", in, got, want)
		}
	}
}
