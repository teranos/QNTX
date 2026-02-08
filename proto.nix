{ pkgs, ... }:

{
  generate-proto-go = {
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

      echo "✓ Plugin proto files generated in plugin/grpc/protocol/"

      # Generate glyph proto (canvas compositions)
      ${pkgs.protobuf}/bin/protoc \
        --plugin=${pkgs.protoc-gen-go}/bin/protoc-gen-go \
        --go_out=. --go_opt=paths=source_relative \
        glyph/proto/canvas.proto

      echo "✓ Glyph proto files generated in glyph/proto/"

      # TODO: Generate proto for atsstore.proto and queue.proto (currently only domain.proto)

      # TODO: Rust proto generation
      # See ADR-006 for strategy: Protocol Buffers as Single Source of Truth
      # - Create qntx-proto crate at crates/qntx-proto/
      # - Use prost for Rust generation
      # - Follow TypeScript pattern from ADR-007 (interfaces only if possible)

      # TODO: Go type migration
      # See ADR-006 for gradual migration approach
      # - Currently generates in plugin/grpc/protocol/
      # - Need to make generated types available as primary types
      # - Handle timestamp format differences (time.Time vs int64)
    '');
  };

  generate-proto-typescript = {
    type = "app";
    program = toString (pkgs.writeShellScript "generate-proto-typescript" ''
      set -e
      echo "Generating TypeScript proto code..."

      # Ensure output directory exists
      mkdir -p web/ts/generated/proto

      # Install ts-proto locally if not present
      if ! [ -d web/node_modules/ts-proto ]; then
        echo "Installing ts-proto..."
        cd web && ${pkgs.bun}/bin/bun add -d ts-proto
        cd ..
      fi

      # Generate TypeScript using ts-proto (minimal - interfaces only)
      # See ADR-007: TypeScript Proto Interfaces-Only Pattern
      # Options to generate ONLY type interfaces:
      # - outputEncodeMethods=false: skip encode/decode functions
      # - outputJsonMethods=false: skip JSON serialization
      # - outputClientImpl=false: skip gRPC client code
      # - outputServices=false: skip service definitions
      # - onlyTypes=true: only generate type definitions
      # - snakeToCamel=false: keep snake_case field names to match Go JSON
      ${pkgs.protobuf}/bin/protoc \
        --plugin=protoc-gen-ts_proto=web/node_modules/.bin/protoc-gen-ts_proto \
        --ts_proto_opt=esModuleInterop=true \
        --ts_proto_opt=outputEncodeMethods=false \
        --ts_proto_opt=outputJsonMethods=false \
        --ts_proto_opt=outputClientImpl=false \
        --ts_proto_opt=outputServices=false \
        --ts_proto_opt=onlyTypes=true \
        --ts_proto_opt=snakeToCamel=false \
        --ts_proto_out=web/ts/generated/proto \
        plugin/grpc/protocol/atsstore.proto

      echo "✓ Plugin proto files generated in web/ts/generated/proto/"

      # Generate TypeScript for glyph proto (canvas compositions)
      ${pkgs.protobuf}/bin/protoc \
        --plugin=protoc-gen-ts_proto=web/node_modules/.bin/protoc-gen-ts_proto \
        --ts_proto_opt=esModuleInterop=true \
        --ts_proto_opt=outputEncodeMethods=false \
        --ts_proto_opt=outputJsonMethods=false \
        --ts_proto_opt=outputClientImpl=false \
        --ts_proto_opt=outputServices=false \
        --ts_proto_opt=onlyTypes=true \
        --ts_proto_opt=snakeToCamel=false \
        --ts_proto_out=web/ts/generated/proto \
        glyph/proto/canvas.proto

      echo "✓ Glyph proto TypeScript files generated in web/ts/generated/proto/"
    '');
  };

  generate-proto = {
    type = "app";
    program = toString (pkgs.writeShellScript "generate-proto" ''
      set -e
      echo "Generating all proto code..."

      # Run Go generation
      nix run .#generate-proto-go

      # Run TypeScript generation
      nix run .#generate-proto-typescript

      echo "✓ All proto files generated"
    '');
  };
}
