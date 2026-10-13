// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package odoo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mechas-Heart/Godoo/internal/project"
	"github.com/Mechas-Heart/Godoo/internal/templates"
	"github.com/Mechas-Heart/Godoo/internal/ui"
)

type option struct{ Key, Value string }

// WriteConf renders .godoo/conf/odoo.conf from godoo.yaml. With addons.yaml
// it also refreshes the selected-modules folder (see ResolveAddons).
func WriteConf(p *project.Project) error {
	addons, err := ResolveAddons(p)
	if err != nil {
		return err
	}
	for _, w := range addons.Warnings {
		ui.Warn("%s", w)
	}
	if err := ApplyLinks(p, addons); err != nil {
		return err
	}
	_, err = templates.WriteFile("odoo.conf.tmpl", p.ConfFile("odoo.conf"), confOptions(p, addons.Path))
	return err
}

func confOptions(p *project.Project, addonsPath []string) []option {
	c := p.Cfg
	o := c.Odoo
	opts := []option{
		{"addons_path", strings.Join(addonsPath, ",")},
		{"data_dir", filepath.Join(p.DataDir(), "odoo")},
		{"admin_passwd", o.AdminPasswd},
		{"db_host", "127.0.0.1"},
		{"db_port", fmt.Sprint(c.Ports.Postgres)},
		{"db_user", c.Postgres.User},
		{"db_password", c.Postgres.Password},
		{"list_db", pyBool(*o.ListDB)},
		{"http_interface", "127.0.0.1"},
		{"http_port", fmt.Sprint(c.Ports.Odoo)},
		{c.Profile.GeventOption(), fmt.Sprint(c.Ports.Gevent)},
		{"proxy_mode", "True"}, // behind nginx
		{"workers", fmt.Sprint(*o.Workers)},
		{"max_cron_threads", fmt.Sprint(*o.MaxCronThreads)},
	}
	if o.DBFilter != "" {
		opts = append(opts, option{"dbfilter", o.DBFilter})
	}

	if c.MailpitOn() {
		opts = append(opts,
			option{"smtp_server", "127.0.0.1"},
			option{"smtp_port", fmt.Sprint(c.Ports.SMTP)},
		)
	} else if s := c.SMTP; s.Server != "" {
		opts = append(opts,
			option{"smtp_server", s.Server},
			option{"smtp_port", fmt.Sprint(s.Port)},
			option{"smtp_user", s.User},
			option{"smtp_password", s.Password},
			option{"smtp_ssl", pyBool(s.SSL)},
		)
		if s.From != "" {
			opts = append(opts, option{"email_from", s.From})
		}
	}

	// odoo.options in godoo.yaml override or extend everything above.
	keys := make([]string, 0, len(o.Options))
	for k := range o.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		replaced := false
		for i := range opts {
			if opts[i].Key == k {
				opts[i].Value, replaced = o.Options[k], true
			}
		}
		if !replaced {
			opts = append(opts, option{k, o.Options[k]})
		}
	}
	return opts
}

func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
