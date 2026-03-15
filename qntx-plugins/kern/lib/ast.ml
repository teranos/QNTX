(* AST for parsed Ax queries.
 *
 * The JSON output must match what dispatch_qntxwasm.go expects:
 * { "subjects": [...], "predicates": [...], "contexts": [...],
 *   "actors": [...], "temporal": { "Since": "..." }, "actions": [...] }
 *
 * Temporal is a tagged union — Rust's serde serializes enums as
 * {"VariantName": value}, so we reproduce that shape. *)

type temporal =
  | Since of string
  | Until of string
  | On of string
  | Between of string * string
  | Over of string

type ax_query = {
  subjects   : string list;
  predicates : string list;
  contexts   : string list;
  actors     : string list;
  temporal   : temporal option;
  actions    : string list;
}

(* Serialize to JSON matching the Rust serde output format *)
let temporal_to_json = function
  | Since s   -> `Assoc [("Since", `String s)]
  | Until s   -> `Assoc [("Until", `String s)]
  | On s      -> `Assoc [("On", `String s)]
  | Between (a, b) -> `Assoc [("Between", `List [`String a; `String b])]
  | Over s    -> `Assoc [("Over", `Assoc [("raw", `String s)])]

let to_json q =
  let strings ss = `List (List.map (fun s -> `String s) ss) in
  `Assoc
    [ ("subjects",   strings q.subjects)
    ; ("predicates", strings q.predicates)
    ; ("contexts",   strings q.contexts)
    ; ("actors",     strings q.actors)
    ; ("temporal",   match q.temporal with
                     | None -> `Null
                     | Some t -> temporal_to_json t)
    ; ("actions",    strings q.actions)
    ]
