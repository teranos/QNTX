{ pkgs, ... }:

{
  generate-proto = {
    type = "app";
    program = toString (pkgs.writeShellScript "generate-proto" ''
      set -e
      echo "Generating gRPC code from proto files..."

      # Use protoc from nixpkgs with Go plugins
      ${pkgs.protobuf}/bin/protoc \
        --plugin=${pkgs.protoc-gen-go}/bin/protoc-gen-go \
        --plugin=${pkgs.protoc-gen-go-grpc}/bin/protoc-gen-go-grpc \
        --go_out=. --go_opt=paths=source_relative \
        --go-grpc_out=. --go-grpc_opt=paths=source_relative \
        plugin/grpc/protocol/domain.proto

      echo "✓ Proto files generated in plugin/grpc/protocol/"

      # TODO: Generate proto for atsstore.proto and queue.proto (currently only domain.proto)

      # TODO: TypeScript proto generation
      # - Use ts-proto for TypeScript generation
      # - Output to web/ts/generated/proto/
      # - Configure proper import paths

      # TODO: Rust proto generation
      # - Create qntx-proto crate at crates/qntx-proto/
      # - Use prost for Rust generation
      # - Set up build.rs for automatic proto compilation

      # TODO: Go type migration
      # - Currently generates in plugin/grpc/protocol/
      # - Need to make generated types available as primary types
      # - Replace ats/types with proto-generated equivalents
    '');
  };
}
