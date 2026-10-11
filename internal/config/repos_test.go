// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

func TestParseRepoSpec(t *testing.T) {
	tests := []struct{ in, url, name, path string }{
		{"web", "https://github.com/OCA/web.git", "web", "./repos/oca/web"},
		{"oca/sale-workflow", "https://github.com/OCA/sale-workflow.git", "sale-workflow", "./repos/oca/sale-workflow"},
		{"acme/odoo-addons", "https://github.com/acme/odoo-addons.git", "odoo-addons", "./repos/acme/odoo-addons"},
		{"https://gitlab.com/acme/tools.git", "https://gitlab.com/acme/tools.git", "tools", "./repos/acme/tools"},
		{"git@github.com:acme/private.git", "git@github.com:acme/private.git", "private", "./repos/acme/private"},
	}
	for _, tt := range tests {
		r, err := ParseRepoSpec(tt.in)
		if err != nil {
			t.Fatalf("%s: %v", tt.in, err)
		}
		if r.URL != tt.url || r.Name != tt.name || r.Path != tt.path {
			t.Errorf("%s: got %+v", tt.in, r)
		}
	}
	for _, bad := range []string{"", "a/b/c"} {
		if _, err := ParseRepoSpec(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestReposDefaults(t *testing.T) {
	c, err := load(t, "name: shop\nodoo: {version: '17'}\n"+ports+"repos:\n  - url: https://github.com/OCA/web.git\n")
	if err != nil {
		t.Fatal(err)
	}
	r := c.Repos[0]
	if r.Name != "web" || r.Branch != "17.0" || r.Path != "./repos/oca/web" {
		t.Errorf("defaults not applied: %+v", r)
	}
}
