// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"path"
	"strings"
)

// Repo is a git repository of addons (OCA or any other) that godoo clones
// and adds to addons_path. `godoo addons add` writes these entries.
type Repo struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"` // default: the odoo version, e.g. "17.0"
	Path   string `yaml:"path"`   // default: ./repos/<owner>/<name>
}

// ParseRepoSpec turns what a user types into a Repo with URL, name and path:
//
//	web                                -> https://github.com/OCA/web.git
//	OCA/web, oca/web                   -> https://github.com/OCA/web.git
//	someone/addons                     -> https://github.com/someone/addons.git
//	https://gitlab.com/acme/addons.git -> as given
//	git@github.com:acme/addons.git     -> as given
func ParseRepoSpec(spec string) (Repo, error) {
	spec = strings.TrimSuffix(strings.TrimSpace(spec), "/")
	if spec == "" {
		return Repo{}, fmt.Errorf("empty repository")
	}
	var url, owner, name string
	switch {
	case strings.Contains(spec, "://") || strings.HasPrefix(spec, "git@"):
		url = spec
		// "git@host:owner/name.git" and "https://host/owner/name.git"
		p := spec[strings.LastIndexAny(spec, ":")+1:]
		if i := strings.Index(p, "//"); i >= 0 {
			p = p[i+2:]
			p = p[strings.Index(p, "/")+1:] // drop host
		}
		owner, name = path.Dir(p), path.Base(p)
	case strings.Count(spec, "/") == 1:
		owner, name = path.Dir(spec), path.Base(spec)
		if strings.EqualFold(owner, "oca") {
			owner = "OCA"
		}
		url = fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
	case !strings.Contains(spec, "/"):
		owner, name = "OCA", spec
		url = fmt.Sprintf("https://github.com/OCA/%s.git", name)
	default:
		return Repo{}, fmt.Errorf("cannot understand repository %q (use NAME, OWNER/NAME or a git URL)", spec)
	}
	name = strings.TrimSuffix(name, ".git")
	if !nameRe.MatchString(strings.ToLower(name)) {
		return Repo{}, fmt.Errorf("cannot derive a name from %q", spec)
	}
	return Repo{Name: name, URL: url, Path: defaultRepoPath(owner, name)}, nil
}

func defaultRepoPath(owner, name string) string {
	owner = strings.ToLower(path.Base(owner))
	if owner == "." || owner == "/" || owner == "" {
		return "./repos/" + name
	}
	return "./repos/" + owner + "/" + name
}

func (c *Config) resolveRepos() error {
	seen := map[string]bool{}
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.URL == "" {
			return fmt.Errorf("repos[%d]: url is required", i)
		}
		if r.Name == "" || r.Path == "" {
			parsed, err := ParseRepoSpec(r.URL)
			if err != nil {
				return fmt.Errorf("repos[%d]: %w", i, err)
			}
			if r.Name == "" {
				r.Name = parsed.Name
			}
			if r.Path == "" {
				r.Path = parsed.Path
			}
		}
		if r.Branch == "" {
			r.Branch = c.Odoo.Version
		}
		if seen[r.Name] {
			return fmt.Errorf("repos: %q is listed twice", r.Name)
		}
		seen[r.Name] = true
	}
	return nil
}

// FindRepo returns the repo with the given name (case-insensitive).
func (c *Config) FindRepo(name string) (Repo, bool) {
	for _, r := range c.Repos {
		if strings.EqualFold(r.Name, name) {
			return r, true
		}
	}
	return Repo{}, false
}
