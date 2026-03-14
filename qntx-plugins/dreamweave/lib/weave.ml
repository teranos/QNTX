(* Weave data extraction and JSON serialization
 *
 * Converts raw ATS attestations (predicate "Weave") into structured
 * JSON for the explorer frontend. *)

open Qntx_dreamweave_proto.Atsstore

module GValue = Google_types.Struct.Google.Protobuf.Value

(* Extract a string value from a protobuf Struct (which is a (string * Value.t option) list) *)
let extract_string (fields : Google_types.Struct.Google.Protobuf.Struct.t) key =
  match List.assoc_opt key fields with
  | Some (Some (`String_value s)) -> Some s
  | _ -> None

(* Extract a number value from a protobuf Struct *)
let extract_number (fields : Google_types.Struct.Google.Protobuf.Struct.t) key =
  match List.assoc_opt key fields with
  | Some (Some (`Number_value n)) -> Some (int_of_float n)
  | _ -> None

(* Convert a single attestation to a Yojson object *)
let attestation_to_json (a : Protocol.Attestation.t) =
  let fields : Google_types.Struct.Google.Protobuf.Struct.t = match a.attributes with
    | Some attrs -> attrs
    | None -> []
  in
  let branch = match a.subjects with
    | b :: _ -> b
    | [] -> ""
  in
  let context = match a.contexts with
    | c :: _ -> c
    | [] -> ""
  in
  `Assoc [
    ("id", `String a.id);
    ("branch", `String branch);
    ("context", `String context);
    ("timestamp", `Int a.timestamp);
    ("text", match extract_string fields "text" with
      | Some s -> `String s
      | None -> `Null);
    ("word_count", match extract_number fields "word_count" with
      | Some n -> `Int n
      | None -> `Null);
    ("turn_count", match extract_number fields "turn_count" with
      | Some n -> `Int n
      | None -> `Null);
  ]

(* Convert a list of attestations into a JSON response *)
let attestations_to_json attestations =
  `Assoc [
    ("weaves", `List (List.map attestation_to_json attestations));
    ("count", `Int (List.length attestations));
  ]

(* Group weaves by branch, returning { branches: { "branch": [weaves] } } *)
let group_by_branch attestations =
  let tbl = Hashtbl.create 32 in
  List.iter (fun (a : Protocol.Attestation.t) ->
    let branch = match a.subjects with
      | b :: _ -> b
      | [] -> "(no branch)"
    in
    let existing = match Hashtbl.find_opt tbl branch with
      | Some l -> l
      | None -> []
    in
    Hashtbl.replace tbl branch (a :: existing)
  ) attestations;
  let branches = Hashtbl.fold (fun branch weaves acc ->
    (branch, `List (List.map attestation_to_json weaves)) :: acc
  ) tbl [] in
  `Assoc [
    ("branches", `Assoc (List.sort (fun (a, _) (b, _) -> String.compare a b) branches));
    ("branch_count", `Int (List.length branches));
    ("total_weaves", `Int (List.length attestations));
  ]
