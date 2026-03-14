{
  description = "QNTX kern plugin - OCaml Ax query parser";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        ocamlPkgs = pkgs.ocamlPackages;

        # ocaml-grpc is not in nixpkgs — package it inline from source.
        # grpc 0.2.0 requires h2 < 0.13.0, so pin h2 to 0.12.0.
        h2-compat = ocamlPkgs.h2.overrideAttrs (old: rec {
          version = "0.12.0";
          src = pkgs.fetchurl {
            url = "https://github.com/anmonteiro/ocaml-h2/releases/download/${version}/h2-${version}.tbz";
            hash = "sha256-u8MmOxpKHiLq7r5QFoKKhGFOQ8gPPMYJ+bfFCtxqd78=";
          };
        });

        h2-lwt-compat = ocamlPkgs.buildDunePackage {
          pname = "h2-lwt";
          version = "0.12.0";
          src = h2-compat.src;
          propagatedBuildInputs = [ h2-compat ocamlPkgs.lwt ocamlPkgs.gluten-lwt ];
        };

        h2-lwt-unix-compat = ocamlPkgs.buildDunePackage {
          pname = "h2-lwt-unix";
          version = "0.12.0";
          src = h2-compat.src;
          propagatedBuildInputs = [
            h2-compat
            h2-lwt-compat
            ocamlPkgs.lwt
            ocamlPkgs.gluten-lwt-unix
            ocamlPkgs.faraday-lwt-unix
          ];
        };

        ocaml-grpc-src = pkgs.fetchurl {
          url = "https://github.com/dialohq/ocaml-grpc/archive/refs/tags/0.2.0.tar.gz";
          hash = "sha256-5dCFcJ+5xWE4VJe/Y4ZpBrVTewfNdGJFx7RvhGShPSk=";
        };

        grpc = ocamlPkgs.buildDunePackage {
          pname = "grpc";
          version = "0.2.0";
          src = ocaml-grpc-src;
          propagatedBuildInputs = [
            h2-compat
            ocamlPkgs.uri
            ocamlPkgs.ppx_deriving
          ];
        };

        grpc-lwt = ocamlPkgs.buildDunePackage {
          pname = "grpc-lwt";
          version = "0.2.0";
          src = ocaml-grpc-src;
          propagatedBuildInputs = [
            grpc
            ocamlPkgs.lwt
            ocamlPkgs.stringext
          ];
        };

        kern = ocamlPkgs.buildDunePackage {
          pname = "kern";
          version = self.rev or "dev";
          src = ./.;

          nativeBuildInputs = [ ocamlPkgs.menhir ];

          buildInputs = [
            ocamlPkgs.menhirLib
            ocamlPkgs.sedlex
            ocamlPkgs.yojson
            ocamlPkgs.ocaml-protoc-plugin
            ocamlPkgs.lwt
            ocamlPkgs.lwt_ppx
            grpc-lwt
            h2-lwt-unix-compat
          ];

          # sedlex requires ppx preprocessing
          propagatedBuildInputs = [ ocamlPkgs.sedlex ];
        };
      in
      {
        packages = {
          default = kern;
          kern-plugin = kern;
        };

        apps.default = {
          type = "app";
          program = "${kern}/bin/qntx-kern";
        };
      });
}
