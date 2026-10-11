// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Mechas-Heart/Godoo/internal/config"
	"github.com/Mechas-Heart/Godoo/internal/nix"
	"github.com/Mechas-Heart/Godoo/internal/odoo"
	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/registry"
	"github.com/Mechas-Heart/Godoo/internal/services"
	"github.com/Mechas-Heart/Godoo/internal/ui"
	"github.com/Mechas-Heart/Godoo/internal/version"
)

func cmdList(ctx context.Context, args []string) error {
	fs := newFlags("list")
	prune := fs.Bool("prune", false, "forget projects whose directory no longer exists")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	entries, err := registry.All()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("no projects yet; create one with `godoo init`")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tODOO\tENV\tSTATE\tURL\tPATH")
	for _, e := range entries {
		p, err := project.Open(e.Path)
		if err != nil {
			if *prune {
				_ = registry.Remove(e.Path)
				continue
			}
			fmt.Fprintf(w, "%s\t?\t?\tmissing\t-\t%s\n", e.Name, e.Path)
			continue
		}
		state := "stopped"
		if services.AnyRunning(p) {
			state = "running"
		}
		c := p.Cfg
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\thttp://localhost:%d\t%s\n", c.Name, c.Odoo.Version, c.Env, state, c.Ports.HTTP, p.Root)
	}
	return w.Flush()
}

func cmdConfig(ctx context.Context, args []string) error {
	if _, _, err := parse(newFlags("config"), args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	out, err := p.Cfg.YAML()
	if err != nil {
		return err
	}
	fmt.Printf("# resolved configuration of %s\n# env %q comes from %s\n%s", p.Root, p.Cfg.Env, p.Cfg.EnvSource, out)
	return nil
}

func cmdDoctor(ctx context.Context, args []string) error {
	fs := newFlags("doctor")
	showVersions := fs.Bool("versions", false, "also list installed versions next to requirements.txt pins")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	ok := true
	check := func(good bool, okMsg, badMsg string) {
		if good {
			fmt.Println("  ok   ", okMsg)
		} else {
			ok = false
			fmt.Println("  FAIL ", badMsg)
		}
	}

	ui.Step("system")
	v, err := nix.Version(ctx)
	check(err == nil, v, fmt.Sprint(err))

	p, err := openProject()
	if err != nil {
		fmt.Println("  -    ", err)
		return nil
	}
	ui.Step("project %s (Odoo %s)", p.Cfg.Name, p.Cfg.Odoo.Version)
	_, err = os.Stat(p.EnvBin("python3"))
	check(err == nil, "nix env built: "+p.EnvDir(), "nix env missing: run `godoo setup`")
	_, err = os.Stat(p.SourceDir() + "/odoo-bin")
	check(err == nil, "odoo source: "+p.SourceDir(), "odoo source missing: run `godoo setup`")

	ui.Step("python packages (odoo/requirements.txt vs nix python)")
	if _, err := os.Stat(p.EnvBin("python3")); err == nil && ok {
		if err := odoo.CheckRequirements(ctx, p, *showVersions); err != nil {
			check(false, "", "missing packages: add them with `godoo pkg add NAME...`")
		}
		missing, err := odoo.MissingPythonDeps(ctx, p, p.AddonsPaths())
		switch {
		case err != nil:
			check(false, "", fmt.Sprint(err))
		case len(missing) == 0:
			check(true, "addons: all external python dependencies available", "")
		default:
			names := make([]string, 0, len(missing))
			for n := range missing {
				names = append(names, n)
			}
			sort.Strings(names)
			check(false, "", "addons need: "+strings.Join(names, ", ")+" (`godoo addons deps --add`)")
		}
	}

	ui.Step("ports")
	for _, st := range services.Status(p) {
		if !st.Enabled {
			continue
		}
		switch {
		case st.Running:
			check(true, fmt.Sprintf("%-8s %d (running, pid %d)", st.Name, st.Port, st.Pid), "")
		default:
			check(config.PortFree(st.Port), fmt.Sprintf("%-8s %d free", st.Name, st.Port),
				fmt.Sprintf("%-8s %d is used by another program; change ports.* in godoo.yaml", st.Name, st.Port))
		}
	}
	if !ok {
		return errors.New("some checks failed")
	}
	return nil
}

func cmdDestroy(ctx context.Context, args []string) error {
	fs := newFlags("destroy")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if _, _, err := parse(fs, args); err != nil {
		return err
	}
	p, err := openProject()
	if err != nil {
		return err
	}
	if !*yes && !confirm(fmt.Sprintf("Delete %s (all databases, filestore, logs)?", p.StateDir())) {
		return errors.New("aborted")
	}
	if err := services.Down(p); err != nil {
		return err
	}
	ui.Step("removing %s", p.StateDir())
	if err := os.RemoveAll(p.StateDir()); err != nil {
		return err
	}
	ui.Info("kept godoo.yaml, nix/, addons/ and the odoo source; `godoo up` starts fresh")
	ui.Info("the nix packages stay in /nix/store until `nix-collect-garbage`")
	return nil
}

func cmdVersion(ctx context.Context, args []string) error {
	fmt.Printf("godoo %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
	return nil
}
