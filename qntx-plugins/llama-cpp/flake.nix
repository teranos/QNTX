{
  description = "QNTX llama-cpp plugin — local LLM inference via llama.cpp";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        # Proto files live outside the plugin directory
        protoSrc = ../../plugin/grpc/protocol;

        # llama.cpp pinned to the same commit as the git submodule
        llama-cpp-src = pkgs.fetchFromGitHub {
          owner = "ggml-org";
          repo = "llama.cpp";
          rev = "db9d8aa428012cc5593e18635d4c3c54095f5138";
          hash = "sha256-ZX5eaeNZYZIzJyEV3k0Dpcr6L84ccm4YRI++pY9hlJU=";
        };

        # GLM — header-only math library for camera/projection
        glm-src = pkgs.fetchFromGitHub {
          owner = "g-truc";
          repo = "glm";
          rev = "1.0.1";
          hash = "sha256-GnGyzNRpzuguc3yYbEFtYLvG+KiCtRAktiN+NvbOICE=";
        };

        llama-cpp-plugin = pkgs.stdenv.mkDerivation {
          pname = "qntx-llama-cpp-plugin";
          version = self.rev or "dev";
          src = ./.;

          postUnpack = ''
            mkdir -p $sourceRoot/vendor
            cp -r ${llama-cpp-src} $sourceRoot/vendor/llama.cpp
            cp -r ${glm-src} $sourceRoot/vendor/glm
          '';

          nativeBuildInputs = with pkgs; [
            cmake
            pkg-config
            grpc
            protobuf
          ];

          buildInputs = with pkgs; [
            grpc
            protobuf
            abseil-cpp
            openssl
            mupdf
          ];

          cmakeFlags = [
            "-DCMAKE_BUILD_TYPE=Release"
            "-DPROTO_DIR=${protoSrc}"
          ];

          doCheck = true;
          checkPhase = ''
            ctest --output-on-failure
          '';
        };
      in
      {
        packages = {
          default = llama-cpp-plugin;
          llama-cpp-plugin = llama-cpp-plugin;
        };

        apps.default = {
          type = "app";
          program = "${llama-cpp-plugin}/bin/qntx-llama-cpp-plugin";
        };
      });
}
