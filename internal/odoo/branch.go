// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/project"
)

// Addon repositories name their branches differently: OCA and odoo/enterprise
// use "18.0", some companies use "18", and single-version repos often just
// have "main". DetectBranch picks the one matching the project's Odoo version.

// Branch is the result of DetectBranch.
type Branch struct {
	Name string
	// Fallback is true when no version branch exists and the repository's
	// default branch was taken; it may not support this Odoo version.
	Fallback bool
}

// DetectBranch asks the remote which branches exist (git ls-remote, no
// clone) and picks, in order: the version ("18.0"), the major ("18"), then
// the default branch the remote's HEAD points to ("main", "master", ...).
func DetectBranch(ctx context.Context, p *project.Project, url, version string) (Branch, error) {
	// --symref reports which branch HEAD points to; the patterns keep pull
	// request and tag refs out (OCA repos have thousands of them).
	out, err := git(ctx, p, "", "ls-remote", "--symref", url, "HEAD", "refs/heads/*")
	if err != nil {
		return Branch{}, fmt.Errorf("cannot list branches of %s (check the URL and your access): %w", url, err)
	}
	heads, defaultBranch := parseLsRemote(out)
	name, fallback, ok := chooseBranch(version, heads, defaultBranch)
	if !ok {
		return Branch{}, fmt.Errorf("%s has no %s branch and no default branch; pass --branch", url, strings.Join(branchCandidates(version), " or "))
	}
	return Branch{Name: name, Fallback: fallback}, nil
}

// branchCandidates lists the version branch names to look for, in order.
func branchCandidates(version string) []string {
	candidates := []string{version} // "18.0"
	if major, ok := strings.CutSuffix(version, ".0"); ok {
		candidates = append(candidates, major) // "18"
	}
	return candidates
}

// chooseBranch applies the order described at DetectBranch.
func chooseBranch(version string, heads []string, defaultBranch string) (name string, fallback, ok bool) {
	for _, c := range branchCandidates(version) {
		if slices.Contains(heads, c) {
			return c, false, true
		}
	}
	if defaultBranch != "" {
		return defaultBranch, true, true
	}
	return "", false, false
}

// parseLsRemote reads `git ls-remote --symref URL HEAD refs/heads/*`:
//
//	ref: refs/heads/main	HEAD
//	3f2a...	HEAD
//	3f2a...	refs/heads/main
//	9c1b...	refs/heads/18.0
func parseLsRemote(out string) (heads []string, defaultBranch string) {
	for _, line := range strings.Split(out, "\n") {
		left, ref, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		if target, isSymref := strings.CutPrefix(left, "ref: "); isSymref && ref == "HEAD" {
			defaultBranch = strings.TrimPrefix(target, "refs/heads/")
			continue
		}
		if name, isHead := strings.CutPrefix(ref, "refs/heads/"); isHead {
			heads = append(heads, name)
		}
	}
	return heads, defaultBranch
}
