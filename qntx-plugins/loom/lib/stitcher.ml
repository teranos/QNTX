(* Stitcher — weaves conversation turns into embedding-ready text blocks
 *
 * Format-agnostic core: stitch_turn accepts pre-parsed turn data (branch,
 * context, label, text) and handles buffering, chunking, and emission.
 *
 * Format-specific parsers call stitch_turn:
 *   - stitch: parses Graunde attestation JSON (UDP listener path)
 *   - jsonl_reader: parses Claude Code JSONL (historical import path)
 *)

(* --- Configuration --- *)

let max_chunk_words = 150

(* --- Per-branch buffer --- *)

(* Hashtbl is OCaml's mutable hash table — like Go's map or JS's Map.
 * We key by branch:context (e.g. "main:session:abc-123") so that
 * concurrent sessions on the same branch get separate buffers.
 * Lists in OCaml are prepend-only (immutable linked lists), so we
 * cons new turns onto the front and reverse when emitting.
 *)
type buffer_entry = {
  context : string;
  turns : string list;
  paths : (string * string) list;  (* (tail, full_path) for edit/read/write/search turns *)
  timestamp : int;  (* latest turn timestamp in ms, 0 = use server time *)
}

let buffers : (string, buffer_entry) Hashtbl.t = Hashtbl.create 16

(* Track last branch per session context to detect branch switches *)
let last_branch : (string, string) Hashtbl.t = Hashtbl.create 16

(* Buffer key combines branch and session context so concurrent
 * sessions on the same branch don't interleave turns. *)
let buffer_key branch context = branch ^ ":" ^ context

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

(* Whitelist of commands that carry semantic meaning for weaves *)
let weave_worthy_prefixes = [
  "git checkout -b"; "git checkout main"; "git checkout master";
  "git add"; "git commit"; "git push";
  "git tag"; "git merge";
  "gh pr create"; "gh pr edit"; "gh pr ready";
  "gh run list"; "gh run view"; "gh run watch"; "gh run rerun";
  "gh issue close"; "gh issue create";
  "make";
]

let is_weave_worthy_command cmd =
  List.exists (fun prefix ->
    let plen = String.length prefix in
    String.length cmd >= plen && String.sub cmd 0 plen = prefix
  ) weave_worthy_prefixes

let extract_tool_command attrs =
  match List.assoc_opt "tool_input" attrs with
  | Some (`Assoc tool_input) ->
    (match List.assoc_opt "command" tool_input with
     | Some (`String cmd) when is_weave_worthy_command cmd -> Some cmd
     | _ -> None)
  | _ -> None

(* Extract file path tail — last two components for embedding readability *)
let file_tail path =
  match String.rindex_opt path '/' with
  | None -> path
  | Some i ->
    let parent_end = i in
    match String.rindex_from_opt path (parent_end - 1) '/' with
    | None -> path
    | Some j -> String.sub path (j + 1) (String.length path - j - 1)

(* Extract tool use as (label, tail_text, full_path option) based on tool_name *)
let extract_tool_use attrs =
  let tool_name = match List.assoc_opt "tool_name" attrs with
    | Some (`String n) -> Some n | _ -> None in
  let tool_input = match List.assoc_opt "tool_input" attrs with
    | Some (`Assoc ti) -> Some ti | _ -> None in
  match tool_name, tool_input with
  | Some "Edit", Some ti ->
    (match List.assoc_opt "file_path" ti with
     | Some (`String fp) -> Some ("edit", file_tail fp, Some fp)
     | _ -> None)
  | Some "Read", Some ti ->
    (match List.assoc_opt "file_path" ti with
     | Some (`String fp) -> Some ("read", file_tail fp, Some fp)
     | _ -> None)
  | Some "Grep", Some ti ->
    (match List.assoc_opt "pattern" ti with
     | Some (`String pat) ->
       let path = match List.assoc_opt "path" ti with
         | Some (`String p) -> p | _ -> "." in
       Some ("search", Printf.sprintf "%s in %s" pat (file_tail path), Some path)
     | _ -> None)
  | Some "Glob", Some ti ->
    (match List.assoc_opt "pattern" ti with
     | Some (`String pat) ->
       let path = match List.assoc_opt "path" ti with
         | Some (`String p) -> p | _ -> "." in
       Some ("search", Printf.sprintf "%s in %s" pat (file_tail path), Some path)
     | _ -> None)
  | Some "Write", Some ti ->
    (match List.assoc_opt "file_path" ti with
     | Some (`String fp) -> Some ("write", file_tail fp, Some fp)
     | _ -> None)
  | Some "Bash", Some ti ->
    (match List.assoc_opt "command" ti with
     | Some (`String cmd) when is_weave_worthy_command cmd -> Some ("tool", cmd, None)
     | _ -> None)
  | _ -> None

(* Extract the conversational text based on event type.
 * UserPromptSubmit → attributes.prompt
 * Stop → attributes.last_assistant_message
 * PreToolUse → attributes.tool_input.command (filtered by whitelist)
 *
 * SessionStart / SessionEnd → session boundary markers
 *
 * PreCompact → inline marker (no flush). Compaction in Claude Code triggers
 *   a session restart (SessionEnd + SessionStart), so the buffer gets flushed
 *   by the subsequent SessionStart anyway. The marker records that turns
 *   before this point were compressed and the original content is lost.
 *
 * SubagentStart / SubagentStop → agent delegation markers (agent_type)
 * TaskCompleted → task completion marker (task_subject) *)
let extract_text json predicate =
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "attributes" fields with
     | Some (`Assoc attrs) ->
       (match predicate with
        | "UserPromptSubmit" ->
          (match List.assoc_opt "prompt" attrs with
           | Some (`String text) -> Some text
           | _ -> None)
        | "Stop" ->
          (match List.assoc_opt "last_assistant_message" attrs with
           | Some (`String text) -> Some text
           | _ -> None)
        | "PreToolUse" | "GraundedPreToolUse" ->
          (* Tool use handled separately via extract_tool_use for label routing *)
          (match extract_tool_use attrs with
           | Some (_, text, _) -> Some text
           | None -> None)
        | "SessionStart" ->
          (match List.assoc_opt "session_id" attrs with
           | Some (`String id) -> Some (Printf.sprintf "Start Session: %s" id)
           | _ -> None)
        | "SessionEnd" ->
          (match List.assoc_opt "session_id" attrs with
           | Some (`String id) -> Some (Printf.sprintf "End Session: %s" id)
           | _ -> None)
        | "PreCompact" ->
          Some "Context compacted"
        | "SubagentStart" ->
          (match List.assoc_opt "agent_type" attrs with
           | Some (`String t) -> Some (Printf.sprintf "Agent started: %s" t)
           | _ -> Some "Agent started")
        | "SubagentStop" ->
          (* TODO: also stitch last_assistant_message from the subagent —
           * skipped for now because it can be very long and would bloat weaves.
           * Consider truncating or summarizing before including. *)
          (match List.assoc_opt "agent_type" attrs with
           | Some (`String t) -> Some (Printf.sprintf "Agent stopped: %s" t)
           | _ -> Some "Agent stopped")
        | "TaskCompleted" ->
          (match List.assoc_opt "task_subject" attrs with
           | Some (`String subj) -> Some (Printf.sprintf "Task completed: %s" subj)
           | _ -> Some "Task completed")
        | "Hook" ->
          (match List.assoc_opt "hook_output" attrs with
           | Some (`String text) -> Some text
           | _ -> None)
        | _ -> None)
     | _ -> None)
  | _ -> None

(* --- Core stitch logic (format-agnostic) --- *)

type stitch_result = {
  branch : string;
  context : string;          (* Session context (e.g. "session:abc-123") *)
  buffered_words : int;
  emitted : string option;  (* Some block when buffer exceeded max_chunk_words *)
  turn_count : int;          (* Number of turns in the emitted block *)
  paths : (string * string) list;  (* (tail, full_path) mapping for frontend hover *)
  timestamp : int;           (* Original timestamp in ms, 0 = use server time *)
}

(* stitch_turn — format-agnostic entry point.
 * Accepts pre-parsed turn data from any format parser (Graunde attestation,
 * Claude Code JSONL, etc.) and handles buffering, chunking, and emission.
 *
 * predicate: controls boundary behavior ("SessionStart" flushes and restarts,
 *            "SessionEnd" forces emit). Other values have no special meaning. *)
let stitch_turn ~branch ~context ~predicate ~label ~text ~paths:turn_path ?(timestamp=0) () =
  let turn = Printf.sprintf "[%s] %s" label text in

  let key = buffer_key branch context in

  (* Branch change: flush old branch's buffer for this session *)
  let branch_flush =
    match Hashtbl.find_opt last_branch context with
    | Some prev when prev <> branch ->
      let old_key = buffer_key prev context in
      (match Hashtbl.find_opt buffers old_key with
       | Some existing when existing.turns <> [] ->
         let block = existing.turns |> List.rev |> String.concat "\n\n" in
         let total_words = buffer_word_count existing.turns in
         let num_turns = List.length existing.turns in
         Hashtbl.remove buffers old_key;
         Printf.printf "[loom] Emitting %d-word block for branch %s (branch change to %s)\n%!"
           total_words prev branch;
         [{ branch = prev; context = existing.context; buffered_words = 0;
            emitted = Some block; turn_count = num_turns; paths = existing.paths;
            timestamp = existing.timestamp }]
       | _ -> [])
    | _ -> []
  in
  Hashtbl.replace last_branch context branch;

  (* SessionStart: flush existing buffer first, then start fresh *)
  let current_result =
    if predicate = "SessionStart" then (
      let emitted =
        match Hashtbl.find_opt buffers key with
        | Some existing when existing.turns <> [] ->
          let block = existing.turns |> List.rev |> String.concat "\n\n" in
          let total_words = buffer_word_count existing.turns in
          let num_turns = List.length existing.turns in
          Printf.printf "[loom] Emitting %d-word block for branch %s (session start)\n%!"
            total_words branch;
          Some (block, existing.context, num_turns, existing.paths, existing.timestamp)
        | _ -> None
      in
      (* Start fresh buffer with the SessionStart marker *)
      Hashtbl.replace buffers key { context; turns = [turn]; paths = turn_path; timestamp };
      match emitted with
      | Some (block, old_context, num_turns, old_paths, old_ts) ->
        { branch; context = old_context; buffered_words = 0; emitted = Some block;
          turn_count = num_turns; paths = old_paths; timestamp = old_ts }
      | None ->
        { branch; context; buffered_words = word_count turn; emitted = None;
          turn_count = 0; paths = []; timestamp }
    ) else (
      (* Get or create buffer for this session, dedup consecutive identical turns *)
      let entry =
        match Hashtbl.find_opt buffers key with
        | Some existing when existing.turns <> [] && List.hd existing.turns = turn ->
          existing (* Skip duplicate *)
        | Some existing ->
          let ts = if timestamp > existing.timestamp then timestamp else existing.timestamp in
          { context; turns = turn :: existing.turns; paths = turn_path @ existing.paths; timestamp = ts }
        | None -> { context; turns = [turn]; paths = turn_path; timestamp }
      in
      let total_words = buffer_word_count entry.turns in

      (* Emit when buffer exceeds threshold or session ends *)
      let should_emit = total_words >= max_chunk_words || predicate = "SessionEnd" in
      if should_emit && total_words > 0 then (
        let block = entry.turns |> List.rev |> String.concat "\n\n" in
        Hashtbl.remove buffers key;
        Printf.printf "[loom] Emitting %d-word block for branch %s (%s)\n%!"
          total_words branch
          (if predicate = "SessionEnd" then "session end" else "threshold");
        let num_turns = List.length entry.turns in
        { branch; context = entry.context; buffered_words = 0; emitted = Some block;
          turn_count = num_turns; paths = entry.paths; timestamp = entry.timestamp }
      ) else (
        Hashtbl.replace buffers key entry;
        { branch; context = entry.context; buffered_words = total_words; emitted = None;
          turn_count = 0; paths = []; timestamp = 0 }
      )
    )
  in
  branch_flush @ [current_result]

(* --- Graunde attestation parser --- *)

(* stitch — parses a Graunde attestation JSON payload and feeds stitch_turn.
 * This is the entry point for the UDP listener (live weaving path). *)
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
    [{ branch = "unknown"; context = "_"; buffered_words = 0; emitted = None; turn_count = 0; paths = []; timestamp = 0 }]
  | Some json ->
    let branch = match extract_branch json with Some b -> b | None -> "unknown" in
    let predicate = match extract_predicate json with Some p -> p | None -> "unknown" in
    let context = match extract_context json with Some c -> c | None -> "_" in
    let text = extract_text json predicate in
    match text with
    | None ->
      [{ branch; context; buffered_words = 0; emitted = None; turn_count = 0; paths = []; timestamp = 0 }]
    | Some text ->
      (* Extract tool use info once for label and path *)
      let tool_info = match predicate with
        | "PreToolUse" | "GraundedPreToolUse" ->
          (match json with
           | `Assoc fields ->
             (match List.assoc_opt "attributes" fields with
              | Some (`Assoc attrs) -> extract_tool_use attrs
              | _ -> None)
           | _ -> None)
        | _ -> None
      in
      let label = match predicate with
        | "UserPromptSubmit" -> "human"
        | "Stop" -> "assistant"
        | "PreToolUse" | "GraundedPreToolUse" ->
          (match tool_info with Some (lbl, _, _) -> lbl | None -> "tool")
        | "SessionStart" | "SessionEnd" -> "session"
        | "PreCompact" -> "compaction"
        | "SubagentStart" | "SubagentStop" -> "agent"
        | "TaskCompleted" -> "task"
        | "Hook" -> "hook"
        | other -> other
      in
      let paths = match tool_info with
        | Some (_, tail, Some full) -> [(tail, full)]
        | _ -> []
      in
      stitch_turn ~branch ~context ~predicate ~label ~text ~paths ()

(* Flush buffers for a specific session context (e.g. "session:abc-123").
 * Used by JSONL import to emit remaining turns without affecting live sessions. *)
let flush_context target_context =
  let results = ref [] in
  let keys_to_remove = ref [] in
  Hashtbl.iter (fun key (entry : buffer_entry) ->
    if entry.context = target_context && entry.turns <> [] then (
      let block = entry.turns |> List.rev |> String.concat "\n\n" in
      let total_words = buffer_word_count entry.turns in
      let num_turns = List.length entry.turns in
      let branch = match String.index_opt key ':' with
        | Some i -> String.sub key 0 i
        | None -> key
      in
      Printf.printf "[loom] Flushing %d-word buffer for %s (import complete)\n%!"
        total_words key;
      results := { branch; context = entry.context; buffered_words = 0;
                   emitted = Some block; turn_count = num_turns; paths = entry.paths;
                   timestamp = entry.timestamp } :: !results;
      keys_to_remove := key :: !keys_to_remove
    )
  ) buffers;
  List.iter (Hashtbl.remove buffers) !keys_to_remove;
  !results

(* Flush all buffered turns as weaves. Called on plugin shutdown
 * to prevent data loss when the server stops. *)
let flush_all () =
  let results = ref [] in
  Hashtbl.iter (fun key entry ->
    if entry.turns <> [] then (
      let block = entry.turns |> List.rev |> String.concat "\n\n" in
      let total_words = buffer_word_count entry.turns in
      let num_turns = List.length entry.turns in
      (* Extract branch from composite key "branch:context" *)
      let branch = match String.index_opt key ':' with
        | Some i -> String.sub key 0 i
        | None -> key
      in
      Printf.printf "[loom] Flushing %d-word buffer for %s (shutdown)\n%!"
        total_words key;
      results := { branch; context = entry.context; buffered_words = 0;
                   emitted = Some block; turn_count = num_turns; paths = entry.paths;
                   timestamp = entry.timestamp } :: !results
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
