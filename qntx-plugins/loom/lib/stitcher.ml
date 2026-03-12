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
 *   predicates: ["UserPromptSubmit"] or ["Stop"] or ["PreToolUse"]
 *   attributes: { "prompt": "..." } or { "last_assistant_message": "..." }
 *               or { "tool_input": { "command": "..." }, ... }
 *)

(* --- Configuration --- *)

let max_chunk_words = 100

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
}

let buffers : (string, buffer_entry) Hashtbl.t = Hashtbl.create 16

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
          extract_tool_command attrs
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
        | "PreToolUse" | "GraundedPreToolUse" -> "tool"
        | "SessionStart" | "SessionEnd" -> "session"
        | "PreCompact" -> "compaction"
        | "SubagentStart" | "SubagentStop" -> "agent"
        | "TaskCompleted" -> "task"
        | other -> other
      in
      let turn = Printf.sprintf "[%s] %s" label text in

      let key = buffer_key branch context in

      (* SessionStart: flush existing buffer first, then start fresh *)
      if predicate = "SessionStart" then (
        let emitted =
          match Hashtbl.find_opt buffers key with
          | Some existing when existing.turns <> [] ->
            let block = existing.turns |> List.rev |> String.concat "\n\n" in
            let total_words = buffer_word_count existing.turns in
            let num_turns = List.length existing.turns in
            Printf.printf "[loom] Emitting %d-word block for branch %s (session start)\n%!"
              total_words branch;
            Some (block, existing.context, num_turns)
          | _ -> None
        in
        (* Start fresh buffer with the SessionStart marker *)
        Hashtbl.replace buffers key { context; turns = [turn] };
        match emitted with
        | Some (block, old_context, num_turns) ->
          { branch; context = old_context; buffered_words = 0; emitted = Some block; turn_count = num_turns }
        | None ->
          Printf.printf "[loom] Buffered %d words for branch %s (%d total)\n%!"
            (word_count turn) branch (word_count turn);
          { branch; context; buffered_words = word_count turn; emitted = None; turn_count = 0 }
      ) else (
        (* Get or create buffer for this session, dedup consecutive identical turns *)
        let entry =
          match Hashtbl.find_opt buffers key with
          | Some existing when existing.turns <> [] && List.hd existing.turns = turn ->
            existing (* Skip duplicate *)
          | Some existing -> { context; turns = turn :: existing.turns }
          | None -> { context; turns = [turn] }
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
          { branch; context = entry.context; buffered_words = 0; emitted = Some block; turn_count = num_turns }
        ) else (
          Hashtbl.replace buffers key entry;
          Printf.printf "[loom] Buffered %d words for branch %s (%d total)\n%!"
            (word_count turn) branch total_words;
          { branch; context = entry.context; buffered_words = total_words; emitted = None; turn_count = 0 }
        )
      )

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
