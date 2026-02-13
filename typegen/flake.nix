{
  description = "QNTX Type Generator - generates TypeScript, Python, Rust, CSS, and Markdown types";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };

        typegen = pkgs.buildGoModule {
          pname = "typegen";
          version = self.rev or "dev";
          src = ./..;

          # Same vendorHash as qntx (shared go.mod)
          # To update: set to `pkgs.lib.fakeHash`, run `nix build ./typegen`, copy the hash from error
          vendorHash = "sha256-Ix5m8m578Jj5mEUy/K1zSWy/wJK9zBO9bGHacPrwOoA=";

          subPackages = [ "cmd/typegen" ];
        };
      in
      {
        packages = {
          default = typegen;
          typegen = typegen;
        };

        checks = {
          typegen-build = typegen;
        };

        apps.default = {
          type = "app";
          program = "${typegen}/bin/typegen";
        };
      });
}
