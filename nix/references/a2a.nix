# A2A's specification, the document, as it is at the commit a2a-go v2.7.0 is generated from.
#
# a2a.proto is what the parity sigil reads of A2A; the document is what says
# how its operations are reached: §5.3 Method Mapping Reference, which
# server/a2a's tests read rather than a table transcribed from it. This
# fetches it by the hash its SOURCE records, and make says writes it to the pin
# as specification.md.
builtins.readFile (builtins.fetchurl {
  url = "https://raw.githubusercontent.com/a2aproject/A2A/7df7685561b19c841c5a1349653df347db8d3314/docs/specification.md";
  sha256 = "91160020b180e0e3769fb9b4f2f23cb96f6c0a9b65115c4abbc9ddb58c6194bc";
})
