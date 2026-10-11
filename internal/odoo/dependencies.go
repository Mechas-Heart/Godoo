// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Mechas-Heart/Godoo/internal/project"
)

//go:embed scan_manifests.py
var scanManifestsScript string

// MissingPythonDeps reads the external_dependencies of every module in dirs
// and returns the python packages the nix python lacks, mapped to the
// modules that need them.
func MissingPythonDeps(ctx context.Context, p *project.Project, dirs []string) (map[string][]string, error) {
	script := filepath.Join(p.RunDir(), "scan_manifests.py")
	if err := os.WriteFile(script, []byte(scanManifestsScript), 0o644); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.EnvBin("python3"), append([]string{script}, dirs...)...)
	cmd.Env = p.Environ()
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("scan manifests: %w", err)
	}
	missing := map[string][]string{}
	if err := json.Unmarshal(out, &missing); err != nil {
		return nil, err
	}
	return missing, nil
}
