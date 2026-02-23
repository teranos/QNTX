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

          # Typegen is now fully standalone (github.com/teranos/typegen)
          src = pkgs.lib.cleanSource ./.;

          # Disable workspace mode for Nix build
          preBuild = ''
            export GOWORK=off
          '';

          # Separate go.mod with minimal dependencies (87 lines in go.sum)
          # To update: set to `pkgs.lib.fakeHash`, run `nix build ./typegen`, copy the hash from error
          vendorHash = pkgs.lib.fakeHash;

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
