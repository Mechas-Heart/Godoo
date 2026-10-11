// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Mechas-Heart/Godoo/internal/project"
)

//go:embed check_requirements.py
var checkRequirementsScript string

// CheckRequirements prints which packages of odoo/requirements.txt are not
// in the nix python, and returns an error if any are missing. Add missing
// ones to nix.python_packages in godoo.yaml.
func CheckRequirements(ctx context.Context, p *project.Project, showVersions bool) error {
	script := filepath.Join(p.RunDir(), "check_requirements.py")
	if err := os.WriteFile(script, []byte(checkRequirementsScript), 0o644); err != nil {
		return err
	}
	args := []string{script, filepath.Join(p.SourceDir(), "requirements.txt")}
	if showVersions {
		args = append(args, "--versions")
	}
	cmd := exec.CommandContext(ctx, p.EnvBin("python3"), args...)
	cmd.Env = p.Environ()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
