// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"slices"
	"testing"
)

func TestChooseBranch(t *testing.T) {
	tests := []struct {
		name         string
		heads        []string
		def          string
		want         string
		wantFallback bool
	}{
		{"exact version wins", []string{"main", "18", "18.0"}, "main", "18.0", false},
		{"major when no .0", []string{"main", "17", "18"}, "main", "18", false},
		{"default branch last", []string{"main", "develop"}, "main", "main", true},
		{"master default", []string{"master"}, "master", "master", true},
	}
	for _, tt := range tests {
		got, fallback, ok := chooseBranch("18.0", tt.heads, tt.def)
		if !ok || got != tt.want || fallback != tt.wantFallback {
			t.Errorf("%s: got %q fallback=%v ok=%v", tt.name, got, fallback, ok)
		}
	}
	if _, _, ok := chooseBranch("18.0", []string{"17.0"}, ""); ok {
		t.Error("no matching branch and no default: expected not ok")
	}
}

func TestBranchCandidates(t *testing.T) {
	if got := branchCandidates("18.0"); !slices.Equal(got, []string{"18.0", "18"}) {
		t.Errorf("18.0: %v", got)
	}
	if got := branchCandidates("saas-17.2"); !slices.Equal(got, []string{"saas-17.2"}) {
		t.Errorf("saas-17.2: %v", got)
	}
}

func TestParseLsRemote(t *testing.T) {
	out := "ref: refs/heads/main\tHEAD\n" +
		"3f2a\tHEAD\n" +
		"3f2a\trefs/heads/main\n" +
		"9c1b\trefs/heads/18.0\n" +
		"77aa\trefs/heads/feature/x\n"
	heads, def := parseLsRemote(out)
	if def != "main" || !slices.Equal(heads, []string{"main", "18.0", "feature/x"}) {
		t.Errorf("heads=%v default=%q", heads, def)
	}
}
