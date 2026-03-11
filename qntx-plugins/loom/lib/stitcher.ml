(* Stitcher — weaves conversation turns into embedding-ready text blocks
 *
 * Triggered by a watcher on UserPromptSubmit and Stop attestations.
 * Each invocation receives one attestation as JSON payload. The stitcher
 * extracts the conversational text (prompt or last_assistant_message),
 * buffers turns per branch, and emits a "woven" block when the buffer
 * exceeds max_chunk_words.
 *
 * The woven block is returned as the ExecuteJob result. The watcher
 * infrastructure or a future ATSStoreService call handles persistence.
 *
 * Attestation structure (from Graunde):
 *   subjects:   ["branch-name"]
 *   predicates: ["UserPromptSubmit"] or ["Stop"]
 *   attributes: { "prompt": "..." } or { "last_assistant_message": "..." }
 *)

(* --- Configuration --- *)

let max_chunk_words = 100

(* --- Per-branch buffer --- *)

(* Hashtbl is OCaml's mutable hash table — like Go's map or JS's Map.
 * We key by branch name and accumulate turns as a list of strings.
 * Lists in OCaml are prepend-only (immutable linked lists), so we
 * cons new turns onto the front and reverse when emitting.
 *)
type buffer_entry = {
  context : string;
  turns : string list;
}

let buffers : (string, buffer_entry) Hashtbl.t = Hashtbl.create 16

let word_count s =
  let len = String.length s in
  if len = 0 then 0
  else
    let count = ref 1 in
    for i = 0 to len - 1 do
      if s.[i] = ' ' || s.[i] = '\n' then incr count
    done;
    !count

let buffer_word_count turns =
  List.fold_left (fun acc s -> acc + word_count s) 0 turns

(* --- JSON extraction --- *)

(* Extract branch name from subjects array *)
let extract_branch json =
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "subjects" fields with
     | Some (`List ((`String branch) :: _)) -> Some branch
     | _ -> None)
  | _ -> None

(* Extract predicate from predicates array *)
let extract_predicate json =
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "predicates" fields with
     | Some (`List ((`String pred) :: _)) -> Some pred
     | _ -> None)
  | _ -> None

(* Extract context from contexts array (e.g. "session:abc-123") *)
let extract_context json =
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "contexts" fields with
     | Some (`List ((`String ctx) :: _)) -> Some ctx
     | _ -> None)
  | _ -> None

(* Extract the conversational text based on event type.
 * UserPromptSubmit → attributes.prompt
 * Stop → attributes.last_assistant_message
 * TODO(#674): Extract semantic content from tool calls (Bash, Edit, etc.) *)
let extract_text json predicate =
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "attributes" fields with
     | Some (`Assoc attrs) ->
       let key = match predicate with
         | "UserPromptSubmit" -> "prompt"
         | "Stop" -> "last_assistant_message"
         | _ -> ""
       in
       (match List.assoc_opt key attrs with
        | Some (`String text) -> Some text
        | _ -> None)
     | _ -> None)
  | _ -> None

(* --- Core stitch logic --- *)

type stitch_result = {
  branch : string;
  context : string;          (* Session context from Graunde (e.g. "session:abc-123") *)
  buffered_words : int;
  emitted : string option;  (* Some block when buffer exceeded max_chunk_words *)
  turn_count : int;          (* Number of turns in the emitted block *)
}

let stitch payload =
  let json =
    try Some (Yojson.Safe.from_string payload)
    with Yojson.Json_error msg ->
      Printf.eprintf "[loom] JSON parse error: %s\n%!" msg;
      None
  in
  match json with
  | None ->
    Printf.printf "[loom] Skipping malformed payload\n%!";
    { branch = "unknown"; context = "_"; buffered_words = 0; emitted = None; turn_count = 0 }
  | Some json ->
    let branch = match extract_branch json with Some b -> b | None -> "unknown" in
    let predicate = match extract_predicate json with Some p -> p | None -> "unknown" in
    let context = match extract_context json with Some c -> c | None -> "_" in
    let text = extract_text json predicate in
    match text with
    | None ->
      { branch; context; buffered_words = 0; emitted = None; turn_count = 0 }
    | Some text ->
      (* Format the turn with a speaker label *)
      let label = match predicate with
        | "UserPromptSubmit" -> "human"
        | "Stop" -> "assistant"
        | other -> other
      in
      let turn = Printf.sprintf "[%s] %s" label text in

      (* Get or create buffer for this branch *)
      let entry =
        match Hashtbl.find_opt buffers branch with
        | Some existing -> { context; turns = turn :: existing.turns }
        | None -> { context; turns = [turn] }
      in
      let total_words = buffer_word_count entry.turns in

      (* Emit when buffer exceeds threshold *)
      if total_words >= max_chunk_words then (
        (* Emit: reverse to chronological order, join into a single block *)
        let block = entry.turns |> List.rev |> String.concat "\n\n" in
        Hashtbl.remove buffers branch;
        Printf.printf "[loom] Emitting %d-word block for branch %s (threshold)\n%!"
          total_words branch;
        let num_turns = List.length entry.turns in
        { branch; context = entry.context; buffered_words = 0; emitted = Some block; turn_count = num_turns }
      ) else (
        Hashtbl.replace buffers branch entry;
        Printf.printf "[loom] Buffered %d words for branch %s (%d total)\n%!"
          (word_count turn) branch total_words;
        { branch; context = entry.context; buffered_words = total_words; emitted = None; turn_count = 0 }
      )

(* Flush all buffered turns as weaves. Called on plugin shutdown
 * to prevent data loss when the server stops. *)
let flush_all () =
  let results = ref [] in
  Hashtbl.iter (fun branch entry ->
    if entry.turns <> [] then (
      let block = entry.turns |> List.rev |> String.concat "\n\n" in
      let total_words = buffer_word_count entry.turns in
      let num_turns = List.length entry.turns in
      Printf.printf "[loom] Flushing %d-word buffer for branch %s (shutdown)\n%!"
        total_words branch;
      results := { branch; context = entry.context; buffered_words = 0;
                   emitted = Some block; turn_count = num_turns } :: !results
    )
  ) buffers;
  Hashtbl.clear buffers;
  !results

(* Serialize a stitch_result to JSON for the ExecuteJob response *)
let result_to_json r =
  let branch_escaped = Yojson.Safe.to_string (`String r.branch) in
  match r.emitted with
  | Some block ->
    let escaped = Yojson.Safe.to_string (`String block) in
    Printf.sprintf {|{"success":true,"result":{"branch":%s,"buffered_words":0,"emitted":%s}}|}
      branch_escaped escaped
  | None ->
    Printf.sprintf {|{"success":true,"result":{"branch":%s,"buffered_words":%d}}|}
      branch_escaped r.buffered_words
