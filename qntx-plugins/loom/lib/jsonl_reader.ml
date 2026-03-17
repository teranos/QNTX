(* Jsonl_reader — parses Claude Code JSONL session files into turns
 *
 * Claude Code writes one JSONL file per session at:
 *   ~/.claude/projects/{project-slug}/{session-uuid}.jsonl
 *
 * Each line is a discrete event with exactly one content block.
 * This module extracts conversational turns and feeds them to
 * Stitcher.stitch_turn for chunking into weaves. *)

(* --- Content block extraction --- *)

(* Extract text from content blocks of type "text" *)
let extract_text_blocks content =
  List.filter_map (fun block ->
    match block with
    | `Assoc fields ->
      (match List.assoc_opt "type" fields, List.assoc_opt "text" fields with
       | Some (`String "text"), Some (`String text) -> Some text
       | _ -> None)
    | _ -> None
  ) content

(* Extract tool_use blocks as (name, input_fields) *)
let extract_tool_use_blocks content =
  List.filter_map (fun block ->
    match block with
    | `Assoc fields ->
      (match List.assoc_opt "type" fields with
       | Some (`String "tool_use") ->
         let name = match List.assoc_opt "name" fields with
           | Some (`String n) -> n | _ -> "unknown" in
         let input = match List.assoc_opt "input" fields with
           | Some (`Assoc i) -> i | _ -> [] in
         Some (name, input)
       | _ -> None)
    | _ -> None
  ) content

(* --- Tool use → turn mapping --- *)

(* Map a tool_use block to (label, display_text, paths) using
 * the same logic as the Graunde path (file_tail, weave_worthy_command). *)
let tool_to_turn name input =
  match name with
  | "Read" ->
    (match List.assoc_opt "file_path" input with
     | Some (`String fp) ->
       let tail = Stitcher.file_tail fp in
       Some ("read", tail, [(tail, fp)])
     | _ -> None)
  | "Edit" ->
    (match List.assoc_opt "file_path" input with
     | Some (`String fp) ->
       let tail = Stitcher.file_tail fp in
       Some ("edit", tail, [(tail, fp)])
     | _ -> None)
  | "Write" ->
    (match List.assoc_opt "file_path" input with
     | Some (`String fp) ->
       let tail = Stitcher.file_tail fp in
       Some ("write", tail, [(tail, fp)])
     | _ -> None)
  | "Grep" | "Glob" ->
    (match List.assoc_opt "pattern" input with
     | Some (`String pat) ->
       let path = match List.assoc_opt "path" input with
         | Some (`String p) -> p | _ -> "." in
       let tail = Stitcher.file_tail path in
       Some ("search", Printf.sprintf "%s in %s" pat tail, [(tail, path)])
     | _ -> None)
  | "Bash" ->
    (match List.assoc_opt "command" input with
     | Some (`String cmd) when Stitcher.is_weave_worthy_command cmd ->
       Some ("tool", cmd, [])
     | _ -> None)
  | _ -> None

(* --- Line processing --- *)

(* Process a single JSONL line into stitch_turn calls.
 * Returns a list of stitch_results (may be empty for skipped lines,
 * or multiple if a line contains both text and tool_use). *)
let process_line json ~branch_override =
  match json with
  | `Assoc fields ->
    let line_type = match List.assoc_opt "type" fields with
      | Some (`String t) -> t | _ -> "" in
    let branch = match branch_override with
      | Some b -> b
      | None ->
        (match List.assoc_opt "gitBranch" fields with
         | Some (`String b) -> b | _ -> "unknown") in
    let session_id = match List.assoc_opt "sessionId" fields with
      | Some (`String s) -> s | _ -> "unknown" in
    let context = "session:" ^ session_id in

    let get_content () =
      match List.assoc_opt "message" fields with
      | Some (`Assoc m) ->
        (match List.assoc_opt "content" m with
         | Some (`List c) -> c | _ -> [])
      | _ -> []
    in

    (match line_type with
     | "user" ->
       let content = get_content () in
       let texts = extract_text_blocks content in
       let text = String.concat "\n\n" texts in
       if String.length text > 0 then
         Stitcher.stitch_turn ~branch ~context ~predicate:"UserPromptSubmit"
           ~label:"human" ~text ~paths:[]
       else
         []

     | "assistant" ->
       let content = get_content () in
       (* Text content → [assistant] turn *)
       let text_results =
         let texts = extract_text_blocks content in
         let text = String.concat "\n\n" texts in
         if String.length text > 0 then
           Stitcher.stitch_turn ~branch ~context ~predicate:"Stop"
             ~label:"assistant" ~text ~paths:[]
         else
           []
       in
       (* Tool_use content → [read]/[edit]/[write]/[search]/[tool] turns *)
       let tool_results = List.concat_map (fun (name, input) ->
         match tool_to_turn name input with
         | Some (label, text, paths) ->
           Stitcher.stitch_turn ~branch ~context ~predicate:"PreToolUse"
             ~label ~text ~paths
         | None -> []
       ) (extract_tool_use_blocks content) in
       text_results @ tool_results

     | _ -> [])  (* Skip progress, file-history-snapshot, system, pr-link, queue-operation *)
  | _ -> []

(* --- File ingestion --- *)

(* Ingest a complete JSONL session file.
 * Reads all lines, feeds turns to stitch_turn, flushes remaining buffer.
 * Returns only stitch_results that emitted weave blocks. *)
let ingest ~file_path ~branch_override =
  let ic = open_in file_path in
  let all_results = ref [] in
  (try
    while true do
      let line = input_line ic in
      if String.length line > 0 then (
        match Yojson.Safe.from_string line with
        | json ->
          let results = process_line json ~branch_override in
          all_results := results @ !all_results
        | exception Yojson.Json_error msg ->
          Printf.eprintf "[loom] JSONL parse error at %s: %s\n%!" file_path msg
      )
    done
  with End_of_file -> ());
  close_in ic;

  (* Flush remaining buffer for this session only *)
  let session_id =
    (* Re-read first line to get sessionId for flush *)
    let ic2 = open_in file_path in
    let sid = (try
      let first_line = input_line ic2 in
      match Yojson.Safe.from_string first_line with
      | `Assoc fields ->
        (match List.assoc_opt "sessionId" fields with
         | Some (`String s) -> "session:" ^ s
         | _ -> "")
      | _ -> ""
    with _ -> "") in
    close_in ic2;
    sid
  in
  let flushed = if String.length session_id > 0
    then Stitcher.flush_context session_id
    else [] in
  let results = List.rev !all_results @ flushed in
  List.filter (fun (r : Stitcher.stitch_result) -> r.emitted <> None) results
