// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/ui"
	"github.com/Mechas-Heart/Godoo/internal/version"
)

// A project records the godoo release it is made with (godoo_version in
// godoo.yaml). A godoo of another v0.X could write different config files
// than the ones you tested, so it warns, and a prod `service install`
// refuses. Newer bug-fix releases of the same v0.X are accepted.
// Development builds have no release to compare and skip the check.

// installHint is how to install a given godoo release.
func installHint(v string) string {
	return "curl -fsSL https://raw.githubusercontent.com/Mechas-Heart/Godoo/main/install.sh | GODOO_VERSION=" + v + " sh"
}

// pinMismatch returns the pinned and running versions when this godoo is
// not compatible with the pin (see version.Compatible: a newer bug-fix
// release of the same v0.X is fine).
func pinMismatch(p *project.Project) (pinned, running string, mismatch bool) {
	running, release := version.Release()
	pinned = version.Normalize(p.Cfg.GodooVersion)
	return pinned, running, release && pinned != "" && !version.Compatible(running, pinned)
}

// warnPin prints a warning when another godoo than the pinned one runs.
func warnPin(p *project.Project) {
	if pinned, running, bad := pinMismatch(p); bad {
		ui.Warn("this project is pinned to godoo %s, but this is %s. Install %s:\n  %s\nor, after checking the project with %s, move the pin: godoo pin",
			pinned, running, pinned, installHint(pinned), running)
	}
}

// refusePinMismatch stops a prod install with another godoo than the pinned
// one: it would write production configs nobody tested.
func refusePinMismatch(p *project.Project) error {
	if pinned, running, bad := pinMismatch(p); bad && p.Cfg.Env == config.EnvProd {
		return fmt.Errorf("env prod and this project is pinned to godoo %s, but this is %s; install %s:\n  %s\n(or move the pin with `godoo pin` after testing %s)",
			pinned, running, pinned, installHint(pinned), running)
	}
	return nil
}

// cmdPin is `godoo pin [VERSION]`: record the godoo this project is made
// with, by default the one running.
func cmdPin(ctx context.Context, args []string) error {
	pos, _, err := parse(newFlags("pin"), args)
	if err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	v, release := version.Release()
	switch {
	case len(pos) > 0:
		v = version.Normalize(pos[0])
		if !version.IsRelease(v) {
			return fmt.Errorf("%q is not a release version like v0.7.1", pos[0])
		}
	case !release:
		return errors.New("this is a development build (" + v + "), which has no release to pin; name one: godoo pin v0.7.1")
	}
	old := version.Normalize(p.Cfg.GodooVersion)
	if old == v {
		ui.Info("already pinned to godoo %s", v)
		return nil
	}
	if err := config.Edit(p.Root, func(root *yaml.Node) error {
		setAfter(root, "godoo_version", v, "env")
		return nil
	}); err != nil {
		return err
	}
	if old == "" {
		ui.Step("pinned to godoo %s", v)
	} else {
		ui.Step("pin moved from godoo %s to %s", old, v)
	}
	ui.Info("commit godoo.yaml; servers install the same version:\n  %s", installHint(v))
	return nil
}

// setAfter sets key in mapping m, adding it right after the `after` key
// when it is new.
func setAfter(m *yaml.Node, key, value, after string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Value = value
			return
		}
	}
	pair := []*yaml.Node{config.Scalar(key, false), config.Scalar(value, false)}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == after {
			m.Content = append(m.Content[:i+2], append(pair, m.Content[i+2:]...)...)
			return
		}
	}
	m.Content = append(m.Content, pair...)
}
