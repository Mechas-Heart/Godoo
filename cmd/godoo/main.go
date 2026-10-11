// Copyright 2026 Mechas Heart
// SPDX-License-Identifier: Apache-2.0

// Command godoo sets up and runs Odoo development environments on Nix.
package main

import (
	"os"

	"github.com/Mechas-Heart/Godoo/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
