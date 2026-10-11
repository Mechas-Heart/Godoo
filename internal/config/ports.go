// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"net"
)

// Ports used by one instance. Everything binds to 127.0.0.1 except nginx and
// the mailpit UI, which use nginx.listen.
type Ports struct {
	HTTP     int `yaml:"http"`     // nginx: what you open in the browser
	Odoo     int `yaml:"odoo"`     // odoo http (behind nginx)
	Gevent   int `yaml:"gevent"`   // odoo websocket/longpolling (workers > 0)
	Postgres int `yaml:"postgres"` // postgres TCP
	SMTP     int `yaml:"smtp"`     // mailpit SMTP
	Mailpit  int `yaml:"mailpit"`  // mailpit web UI
}

// PortsForSlot returns the port set for the n-th instance on this machine.
// Slot 0 gives the familiar http://localhost:8069 and mailpit on :8025.
func PortsForSlot(n int) Ports {
	internal := 20000 + n*10
	return Ports{
		HTTP:     8069 + n,
		Odoo:     internal,
		Gevent:   internal + 2,
		Postgres: 5433 + n, // 5432 is left for a system postgres
		SMTP:     1025 + n,
		Mailpit:  8025 + n,
	}
}

func (p Ports) List() []int {
	return []int{p.HTTP, p.Odoo, p.Gevent, p.Postgres, p.SMTP, p.Mailpit}
}

// AllocatePorts picks the first slot whose ports are not reserved by another
// registered instance and are currently free on this machine.
func AllocatePorts(reserved map[int]bool) (Ports, error) {
	for n := range 100 {
		p := PortsForSlot(n)
		ok := true
		for _, port := range p.List() {
			if reserved[port] || !PortFree(port) {
				ok = false
				break
			}
		}
		if ok {
			return p, nil
		}
	}
	return Ports{}, errors.New("no free port slot found")
}

// PortFree reports whether nothing is listening on 127.0.0.1:port.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func (p Ports) validate() error {
	seen := map[int]bool{}
	for _, port := range p.List() {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("ports: %d is not a valid port (all of http, odoo, gevent, postgres, smtp, mailpit must be set)", port)
		}
		if seen[port] {
			return fmt.Errorf("ports: %d is used twice", port)
		}
		seen[port] = true
	}
	return nil
}
