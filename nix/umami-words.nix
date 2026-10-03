# What Umami's API document says of its schemas, and nothing else of it.
#
# The parity sigil reads the words of a few of Umami's API schemas
# (server/parity/umami_v3.3.1_ca661c7/words). The document they come from is
# 1.4 MB; this fetches it by the hash its SOURCE records and keeps, of every
# schema, its description and each property's. make says writes the result to
# the pin as openapi.words.json.
let
  doc = builtins.fromJSON (builtins.readFile (builtins.fetchurl {
    url = "https://raw.githubusercontent.com/umami-software/docs/a50c0af47f8747d9b0cf99b59c4cbf3659407285/openapi.json";
    sha256 = "09986e60be747a0b7bff3d57a61b91939bf74e8b02d180857384d2e11e3fa1e2";
  }));
  says = schema: {
    description = schema.description or "";
    properties = builtins.mapAttrs (_: property: { description = property.description or ""; }) (schema.properties or { });
  };
in
builtins.toJSON { components.schemas = builtins.mapAttrs (_: says) doc.components.schemas; }
