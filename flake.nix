# Copyright 2026 Mechas Heart
# SPDX-License-Identifier: Apache-2.0
#
# godoo itself as a Nix package, so Nix users need neither Go nor a download:
#
#   nix run github:Mechas-Heart/Godoo -- init shop --version 18.0
#   nix profile install github:Mechas-Heart/Godoo
#   nix develop          # a shell with Go and make, for working on godoo
#
# This is not the per-project flake: that one is rendered from
# internal/templates/flake.nix.tmpl into each project's nix/ folder.
{
  description = "godoo: Odoo development environments on Nix";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";

  outputs = { self, nixpkgs }:
    let
      # Bump together with the release tag: the release workflow refuses a
      # tag that does not match.
      version = "0.7.1";

      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # Commit time, 20261013152653 -> 2026-10-13T15:26:53Z (as `make build`).
      date =
        let d = self.lastModifiedDate or "19700101000000"; at = start: len: builtins.substring start len d;
        in "${at 0 4}-${at 4 2}-${at 6 2}T${at 8 2}:${at 10 2}:${at 12 2}Z";
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "godoo";
          inherit version;
          src = self;
          subPackages = [ "cmd/godoo" ];
          # Hash of the Go dependencies (go.sum). When go.sum changes, nix
          # build fails and prints the new hash to put here.
          vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
          env.CGO_ENABLED = 0;
          # Same metadata as `make build` (see internal/version).
          ldflags = [
            "-s" "-w"
            "-X github.com/Mechas-Heart/Godoo/internal/version.Version=v${version}"
            "-X github.com/Mechas-Heart/Godoo/internal/version.Commit=${self.shortRev or self.dirtyShortRev or "none"}"
            "-X github.com/Mechas-Heart/Godoo/internal/version.Date=${date}"
          ];
          meta = {
            description = "Odoo development environments on Nix";
            homepage = "https://github.com/Mechas-Heart/Godoo";
            license = pkgs.lib.licenses.asl20;
            mainProgram = "godoo";
          };
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell { packages = [ pkgs.go pkgs.gnumake pkgs.gopls ]; };
      });
    };
}
