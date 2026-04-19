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
        plugin/grpc/protocol/domain.proto \
        plugin/grpc/protocol/atsstore.proto \
        plugin/grpc/protocol/queue.proto \
        plugin/grpc/protocol/schedule.proto \
        plugin/grpc/protocol/server.proto \
        plugin/grpc/protocol/fileservice.proto \
        plugin/grpc/protocol/llm.proto \
        plugin/grpc/protocol/embedding.proto \
        plugin/grpc/protocol/search.proto \
        plugin/grpc/protocol/vectorsearch.proto \
        plugin/grpc/protocol/ground.proto

      echo "✓ Plugin proto files generated in plugin/grpc/protocol/"

      # Generate glyph proto (canvas compositions + events)
      ${pkgs.protobuf}/bin/protoc \
        --plugin=${pkgs.protoc-gen-go}/bin/protoc-gen-go \
        --go_out=. --go_opt=paths=source_relative \
        glyph/proto/canvas.proto \
        glyph/proto/events.proto \
        glyph/proto/files.proto

      echo "✓ Glyph proto files generated in glyph/proto/"

      # Rust proto types are generated at build time via prost (see crates/qntx-proto/build.rs)
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
        plugin/grpc/protocol/atsstore.proto \
        plugin/grpc/protocol/server.proto

      echo "✓ Plugin proto files generated in web/ts/generated/proto/"

      # Generate TypeScript for glyph proto (canvas compositions + events)
      # useDate=string: google.protobuf.Timestamp → string (ISO 8601, matches Go JSON output)
      ${pkgs.protobuf}/bin/protoc \
        --plugin=protoc-gen-ts_proto=web/node_modules/.bin/protoc-gen-ts_proto \
        --ts_proto_opt=esModuleInterop=true \
        --ts_proto_opt=outputEncodeMethods=false \
        --ts_proto_opt=outputJsonMethods=false \
        --ts_proto_opt=outputClientImpl=false \
        --ts_proto_opt=outputServices=false \
        --ts_proto_opt=onlyTypes=true \
        --ts_proto_opt=snakeToCamel=false \
        --ts_proto_opt=useDate=string \
        --ts_proto_out=web/ts/generated/proto \
        glyph/proto/canvas.proto \
        glyph/proto/events.proto \
        glyph/proto/files.proto

      echo "✓ Glyph proto TypeScript files generated in web/ts/generated/proto/"
    '');
  };

  generate-proto-ocaml = {
    type = "app";
    program = toString (pkgs.writeShellScript "generate-proto-ocaml" ''
      set -e
      echo "Generating OCaml proto code..."

      PROTOC_GEN_OCAML="$HOME/.opam/5.2.1/bin/protoc-gen-ocaml"
      if ! [ -x "$PROTOC_GEN_OCAML" ]; then
        echo "⚠ protoc-gen-ocaml not found at $PROTOC_GEN_OCAML — skipping OCaml proto generation"
        exit 0
      fi

      ${pkgs.protobuf}/bin/protoc \
        --plugin=protoc-gen-ocaml="$PROTOC_GEN_OCAML" \
        --ocaml_out=plugin/grpc/ocaml/proto/ \
        plugin/grpc/protocol/domain.proto \
        plugin/grpc/protocol/atsstore.proto \
        plugin/grpc/protocol/embedding.proto \
        plugin/grpc/protocol/llm.proto \
        plugin/grpc/protocol/vectorsearch.proto \
        plugin/grpc/protocol/search.proto

      echo "✓ OCaml proto files generated in plugin/grpc/ocaml/proto/"
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

      # Run OCaml generation
      nix run .#generate-proto-ocaml

      # C++ protos are generated at build time via shared proto.cmake
      # (protoc version must match the linked protobuf library)

      echo "✓ All proto files generated"
    '');
  };

  # C++ proto generation is handled by plugin/grpc/protocol/proto.cmake (shared
  # cmake include) because protoc version must match the linked protobuf library.
  # Go, TypeScript, and OCaml don't have this constraint.

  # TODO: libstelt — shared C++ library for plugin infrastructure.
  # All 5 plugins duplicate base64, pdf_extract, LLMClient, ATSClient, and
  # main() boilerplate. libstelt extracts these into a single linkable library.
  # Plugins link against libstelt and only implement domain logic.
}
