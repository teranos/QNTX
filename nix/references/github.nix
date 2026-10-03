# GitHub's REST API, as GitHub describes it, narrowed to what GitHubService
# (ADR-043) calls of it.
#
# The description of API version 2026-03-10, the one GitHubService sends, is
# 13 MB. This fetches it by the hash its SOURCE records and keeps, of each
# operation a message of ours names (operations.json, which make says writes
# from plugin/grpc/protocol/github_*.proto), its words, its path and query
# parameters, its JSON body and what its 2xx answers with; and every schema
# those reach. Examples are left out, and so is every key of a schema that is
# not its words or its shape. make says writes the result to the pin as
# openapi.json, which the parity sigil reads as the github reference.
#
# Nix keeps an object's keys in order of their names, so the pin does too:
# a model's columns are in that order, not the description's.
let
  pin = ../../server/parity/github_2026-03-10_7bdf5f0;
  doc = builtins.fromJSON (builtins.readFile (builtins.fetchurl {
    url = "https://raw.githubusercontent.com/github/rest-api-description/7bdf5f00553689c2dabb027685cf29081093767e/descriptions/api.github.com/api.github.com.2026-03-10.json";
    sha256 = "2d3c97fbbcff73734b9e53329837d7a6d0c857dc81aa47b117082c843a03f22e";
  }));
  operations = builtins.fromJSON (builtins.readFile (pin + "/operations.json"));

  # What a $ref under components names: #/components/parameters/per-page.
  named = kind: ref:
    let prefix = "#/components/${kind}/"; in
    if builtins.substring 0 (builtins.stringLength prefix) ref == prefix
    then builtins.substring (builtins.stringLength prefix) (builtins.stringLength ref) ref
    else throw "${ref} is not under #/components/${kind}/";
  # A parameter, body or answer as it is, when it is written by $ref.
  read = kind: x: if x ? "$ref" then doc.components.${kind}.${named kind x."$ref"} else x;

  # A schema's words and shape, and nothing else of it.
  shape = { "$ref" = null; type = null; title = null; description = null; required = null; nullable = null;
            properties = null; items = null; additionalProperties = null; allOf = null; anyOf = null; oneOf = null; };
  kept = s:
    let s' = builtins.intersectAttrs shape s; in
    s'
    // (if s' ? properties then { properties = builtins.mapAttrs (_: kept) s'.properties; } else { })
    // (if s' ? items then { items = kept s'.items; } else { })
    // (if builtins.isAttrs (s'.additionalProperties or null) then { additionalProperties = kept s'.additionalProperties; } else { })
    // builtins.listToAttrs (map (k: { name = k; value = map kept s'.${k}; }) (builtins.filter (k: s' ? ${k}) [ "allOf" "anyOf" "oneOf" ]));

  # The schemas a value refers to, anywhere in it.
  refs = x:
    if builtins.isAttrs x then
      (if x ? "$ref" && builtins.isString x."$ref" then [ (named "schemas" x."$ref") ] else [ ])
      ++ builtins.concatMap refs (builtins.attrValues (removeAttrs x [ "$ref" ]))
    else if builtins.isList x then builtins.concatMap refs x
    else [ ];

  json = content: if content ? "application/json" then { "application/json".schema = kept (content."application/json".schema or { }); } else { };
  operation = o:
    let
      op = doc.paths.${o.path}.${o.method};
      answered = builtins.filter (code: builtins.substring 0 1 code == "2") (builtins.attrNames op.responses);
    in
    builtins.intersectAttrs { summary = null; description = null; operationId = null; } op // {
      parameters = map (p: let q = read "parameters" p; in
        { inherit (q) name "in"; description = q.description or ""; required = q.required or false; schema = kept (q.schema or { }); })
        (op.parameters or [ ]);
      responses = builtins.listToAttrs (map (code: let r = read "responses" op.responses.${code}; in
        { name = code; value = { description = r.description or ""; content = json (r.content or { }); }; }) answered);
    } // (if op ? requestBody then { requestBody.content = json (read "requestBodies" op.requestBody).content; } else { });

  paths = builtins.foldl' (acc: o: acc // { ${o.path} = (acc.${o.path} or { }) // { ${o.method} = operation o; }; }) { } operations;
  reached = builtins.genericClosure {
    startSet = map (key: { inherit key; }) (refs paths);
    operator = item: map (key: { inherit key; }) (refs doc.components.schemas.${item.key});
  };
in
builtins.toJSON {
  inherit (doc) openapi;
  info = { inherit (doc.info) title version; };
  inherit paths;
  components.schemas = builtins.listToAttrs (map (r: { name = r.key; value = kept doc.components.schemas.${r.key}; }) reached);
}
