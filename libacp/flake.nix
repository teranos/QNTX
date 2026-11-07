{
  description = "libacp: the Agent Client Protocol, agent and client side, in C11 with a C ABI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      # 「[Opus 5.5] A consumer that holds its own json-c builds libacp against that one. Clang 22.1.3 and later:」
      # 「[Opus 5.5] an older compiler-rt deadlocks ASan at start on macOS 26.4 and later (llvm/llvm-project#182943).」
      libacpWith = pkgs: jsonC: pkgs.llvmPackages_22.stdenv.mkDerivation {
        pname = "libacp";
        version = self.shortRev or "dirty";
        src = ./.;
        nativeBuildInputs = [ pkgs.pkg-config ];
        buildInputs = [ jsonC ];
        buildPhase = "make";
        doCheck = true;
        # 「[Opus 5.5] The method names are generated from schema/v1/meta.json, which is the release's own, byte for byte.」
        checkPhase = "cmp schema/v1/meta.json ${meta pkgs} && make test";
        installPhase = "make install PREFIX=$out";
      };
      # 「[Opus 5.5] github.com/agentclientprotocol/agent-client-protocol schema-v1.26.0, the release's meta.json:」
      # 「[Opus 5.5] every method by its name, as the TypeScript SDK generates its method tables from it.」
      meta = pkgs: pkgs.fetchurl {
        name = "acp-1.26.0-meta.json";
        url = "https://github.com/agentclientprotocol/agent-client-protocol/releases/download/schema-v1.26.0/meta.json";
        sha256 = "061edb6efa8fb2aa2792459a86ec7268de5fe665bba48b2ffe7939df01481f88";
      };
    in {
      lib = { inherit libacpWith; };

      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = libacpWith pkgs pkgs.json_c;
          libacp = libacpWith pkgs pkgs.json_c;
        });

      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell.override { stdenv = pkgs.llvmPackages_22.stdenv; } {
            packages = [ pkgs.pkg-config pkgs.json_c ];
          };
        });
    };
}
