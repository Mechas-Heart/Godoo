// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package project

import (
	"os"
	"path/filepath"
)

// ModulesIn lists the module directories (with a manifest) directly in dir,
// sorted by name. Symlinks to module directories count too (.godoo/addons).
func ModulesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var mods []string
	for _, e := range entries {
		// no e.IsDir(): a symlink entry is not a dir, but os.Stat below
		// follows it, so linked modules are found too
		for _, manifest := range []string{"__manifest__.py", "__openerp__.py"} {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), manifest)); err == nil {
				mods = append(mods, e.Name())
				break
			}
		}
	}
	return mods
}
