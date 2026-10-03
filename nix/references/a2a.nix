# A2A's specification, the document, as it is at v1.0.1.
#
# a2a.proto is what the parity sigil reads of A2A; the document is what says
# how its operations are reached: §5.3 Method Mapping Reference, which
# server/a2a's tests read rather than a table transcribed from it. This
# fetches it by the hash its SOURCE records, and make says writes it to the pin
# as specification.md.
builtins.readFile (builtins.fetchurl {
  url = "https://raw.githubusercontent.com/a2aproject/A2A/3303592588e388e62e0f69f701af531d2f4e3991/docs/specification.md";
  sha256 = "627ccfe6ffb1be2c56811c3dcc18780deb419f41cdc98248095c939b2d9dc9cb";
})
