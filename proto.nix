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
    '');
  };
}
