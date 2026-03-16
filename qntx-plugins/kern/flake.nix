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
        # grpc 0.2.0 requires h2 < 0.13.0, so build h2 0.12.0 from scratch.
        h2-src = pkgs.fetchurl {
          url = "https://github.com/anmonteiro/ocaml-h2/releases/download/0.12.0/h2-0.12.0.tbz";
          hash = "sha256-NuQLET2Q6jg2GajHvZk/hmExw8XZV2GbaEnrMq+MU8Y=";
        };

        h2-compat = ocamlPkgs.buildDunePackage {
          pname = "h2";
          version = "0.12.0";
          src = h2-src;
          propagatedBuildInputs = [
            ocamlPkgs.angstrom
            ocamlPkgs.faraday
            ocamlPkgs.base64
            ocamlPkgs.bigstringaf
            ocamlPkgs.httpun-types
            ocamlPkgs.psq
            ocamlPkgs.hpack
          ];
        };

        h2-lwt-compat = ocamlPkgs.buildDunePackage {
          pname = "h2-lwt";
          version = "0.12.0";
          src = h2-src;
          propagatedBuildInputs = [ h2-compat ocamlPkgs.lwt ocamlPkgs.gluten-lwt ];
        };

        h2-lwt-unix-compat = ocamlPkgs.buildDunePackage {
          pname = "h2-lwt-unix";
          version = "0.12.0";
          src = h2-src;
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
          hash = "sha256-5myWWT3qziJ9m84aRXodGRZ5sGlUNBcY/6nkkzJ4in4=";
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

        # nixpkgs has 4.5.0, we need 6.2.0 (generated proto uses Runtime'.Spec.Message).
        # Only build the runtime library (-p ocaml-protoc-plugin), not the protoc plugin
        # binary which needs omd >= 2.0 (nixpkgs has 1.3.2) and pkg-config for protobuf.
        ocaml-protoc-plugin = ocamlPkgs.buildDunePackage {
          pname = "ocaml-protoc-plugin";
          version = "6.2.0";
          src = pkgs.fetchurl {
            url = "https://github.com/andersfugmann/ocaml-protoc-plugin/releases/download/6.2.0/ocaml-protoc-plugin-6.2.0.tbz";
            hash = "sha256-Rqh3iNOeCdXoJPyrKXGPliV4f1sP+4+tO7QSVnwB7PY=";
          };
          # We only need the runtime library, not the protoc plugin binary or
          # google_types (which need omd >= 2.0, protoc, pkg-config).
          # Remove dirs that pull in those deps before dune sees them.
          postPatch = ''
            rm -rf src/plugin src/google_types test
          '';
          propagatedBuildInputs = [
            ocamlPkgs.base64
            ocamlPkgs.ptime
            ocamlPkgs.ppx_expect
            ocamlPkgs.ppx_inline_test
          ];
          doCheck = false;
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
            ocaml-protoc-plugin
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
