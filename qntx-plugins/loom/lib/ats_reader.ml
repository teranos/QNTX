(* ATS reader — weaves OTLPSpan attestations from ATS into embedding blocks
 *
 * Third ingestion path alongside Graunde UDP (live) and JSONL (historical).
 * Reads OTLPSpan attestations written by the ix-otlp plugin, groups them
 * by trace, sorts by start_time_ns, maps spans to turns, and feeds the
 * stitcher pipeline.
 *
 * Runs on loom startup to catch up on all unweaved traces, then can be
 * triggered via HTTP endpoint for on-demand import. *)

open Qntx_plugin_proto.Atsstore
module GValue = Qntx_plugin_proto.Struct.Google.Protobuf.Value

(* --- Attribute extraction from protobuf Struct --- *)

let extract_string fields key =
  match List.assoc_opt key fields with
  | Some (Some (`String_value s)) -> s
  | _ -> ""

let extract_number fields key =
  match List.assoc_opt key fields with
  | Some (Some (`Number_value n)) -> int_of_float n
  | _ -> 0

(* --- Span → turn mapping --- *)

(* Derive label and text from OTLPSpan attributes.
 * Returns None if the span has no weave-worthy content. *)
let span_to_turn fields =
  let name = extract_string fields "name" in
  let op_name = extract_string fields "gen_ai.operation.name" in
  let agent_name = extract_string fields "gen_ai.agent.name" in
  let model = extract_string fields "gen_ai.request.model" in
  let prompt = extract_string fields "gen_ai.prompt" in
  let completion = extract_string fields "gen_ai.completion" in
  let input_msgs = extract_string fields "gen_ai.input.messages" in
  let output_msgs = extract_string fields "gen_ai.output.messages" in
  let tool_input = extract_string fields "gen_ai.tool.input" in

  (* Agent invocation → session boundary *)
  if op_name = "invoke_agent" || op_name = "create_agent" then
    let display = if agent_name <> "" then agent_name else name in
    Some ("session", Printf.sprintf "Agent: %s" display, [])

  (* Chat/completion → [human] + [assistant] *)
  else if op_name = "chat" || op_name = "text_completion" then
    let model_tag = if model <> "" then model else "unknown" in
    let assistant_text =
      if completion <> "" then Printf.sprintf "[%s] %s" model_tag completion
      else if output_msgs <> "" then Printf.sprintf "[%s] %s" model_tag output_msgs
      else Printf.sprintf "[%s] (completion)" model_tag
    in
    let human_text =
      if prompt <> "" then prompt
      else if input_msgs <> "" then input_msgs
      else ""
    in
    Some ("assistant", assistant_text, [("_human_text", human_text)])

  (* Tool spans: name starts with "tool." *)
  else if String.length name > 5 && String.sub name 0 5 = "tool." then
    let tool_name = String.sub name 5 (String.length name - 5) in
    let input_text = if tool_input <> "" then tool_input else tool_name in
    let (label, display, paths) = match tool_name with
      | "read_file" | "read" ->
        let tail = Stitcher.file_tail input_text in
        ("read", tail, [(tail, input_text)])
      | "edit_file" | "edit" | "write_file" | "write" ->
        let tail = Stitcher.file_tail input_text in
        ("edit", tail, [(tail, input_text)])
      | "search" | "grep" | "glob" | "find" ->
        ("search", input_text, [])
      | "bash" | "shell" | "run_command" ->
        if Stitcher.is_weave_worthy_command input_text then
          ("tool", input_text, [])
        else
          ("tool", tool_name, [])
      | _ ->
        ("tool", Printf.sprintf "%s: %s" tool_name input_text, [])
    in
    Some (label, display, paths)

  (* Generic agent spans *)
  else if agent_name <> "" then
    Some ("agent", Printf.sprintf "%s: %s" agent_name name, [])

  (* Unknown → skip *)
  else
    None

(* --- Trace processing --- *)

(* Process a group of OTLPSpan attestations belonging to the same trace.
 * Sorts by start_time_ns, maps to turns, feeds stitcher. *)
let process_trace trace_id attestations =
  (* Sort by start_time_ns *)
  let sorted = List.sort (fun (a : Protocol.Attestation.t) (b : Protocol.Attestation.t) ->
    let fields_a = match a.attributes with Some f -> f | None -> [] in
    let fields_b = match b.attributes with Some f -> f | None -> [] in
    let ts_a = extract_number fields_a "start_time_ns" in
    let ts_b = extract_number fields_b "start_time_ns" in
    compare ts_a ts_b
  ) attestations in

  (* Derive branch from first span's subject *)
  let branch = match sorted with
    | a :: _ -> (match a.subjects with b :: _ -> b | [] -> "agno:agent")
    | [] -> "agno:agent"
  in
  let context = "trace:" ^ trace_id in

  let all_results = ref [] in

  (* Emit SessionStart *)
  let start_results = Stitcher.stitch_turn
    ~branch ~context ~predicate:"SessionStart"
    ~label:"session" ~text:(Printf.sprintf "Trace: %s" trace_id)
    ~paths:[] () in
  all_results := start_results @ !all_results;

  (* Process each span *)
  List.iter (fun (a : Protocol.Attestation.t) ->
    let fields = match a.attributes with Some f -> f | None -> [] in
    let start_time_ns = extract_number fields "start_time_ns" in
    let timestamp = start_time_ns / 1_000_000 in (* ns → ms *)

    match span_to_turn fields with
    | None -> ()
    | Some (label, text, meta) ->
      (* If this is a chat span with human input, emit human turn first *)
      let human_text = match List.assoc_opt "_human_text" meta with
        | Some ht when ht <> "" -> Some ht
        | _ -> None
      in
      (match human_text with
       | Some ht ->
         let human_results = Stitcher.stitch_turn
           ~branch ~context ~predicate:"UserPromptSubmit"
           ~label:"human" ~text:ht ~paths:[] ~timestamp () in
         all_results := human_results @ !all_results
       | None -> ());
      (* Filter out _human_text from paths *)
      let paths = List.filter (fun (k, _) -> k <> "_human_text") meta in
      let predicate = match label with
        | "session" -> "SessionStart"
        | "human" -> "UserPromptSubmit"
        | "assistant" -> "Stop"
        | _ -> "PreToolUse"
      in
      let results = Stitcher.stitch_turn
        ~branch ~context ~predicate
        ~label ~text ~paths ~timestamp () in
      all_results := results @ !all_results
  ) sorted;

  (* Emit SessionEnd *)
  let end_results = Stitcher.stitch_turn
    ~branch ~context ~predicate:"SessionEnd"
    ~label:"session" ~text:"Trace completed"
    ~paths:[] () in
  all_results := end_results @ !all_results;

  List.rev !all_results

(* --- Query and weave OTLPSpan attestations --- *)

(* Query ATS for all OTLPSpan attestations, group by trace, weave each trace.
 * Returns stitch_results with emitted weave blocks. *)
let ingest () =
  let open Lwt.Syntax in
  (* Query for OTLPSpan attestations *)
  let filter = Protocol.AttestationFilter.make
    ~predicates:["OTLPSpan"]
    () in
  let request = Protocol.GetAttestationsRequest.make
    ~auth_token:!Ats_client.auth_token
    ~filter
    () in
  let request_bytes = Qntx_plugin.Server.proto_to_string
    (Protocol.GetAttestationsRequest.to_proto request) in

  let* result = Ats_client.grpc_call
    ~path:"/protocol.ATSStoreService/GetAttestations"
    ~request_bytes in

  match result with
  | Error msg ->
    Printf.eprintf "[loom] ATS reader: failed to query OTLPSpan attestations: %s\n%!" msg;
    Lwt.return []
  | Ok payload ->
    let reader = Ocaml_protoc_plugin.Reader.create payload in
    match Protocol.GetAttestationsResponse.from_proto reader with
    | Ok resp when resp.success ->
      let attestations = resp.attestations in
      Printf.printf "[loom] ATS reader: found %d OTLPSpan attestations\n%!" (List.length attestations);

      if List.length attestations = 0 then
        Lwt.return []
      else (
        (* Group by trace_id (context field = "trace:{id}") *)
        let trace_table = Hashtbl.create 16 in
        List.iter (fun (a : Protocol.Attestation.t) ->
          let context = match a.contexts with c :: _ -> c | [] -> "unknown" in
          let existing = match Hashtbl.find_opt trace_table context with
            | Some l -> l | None -> [] in
          Hashtbl.replace trace_table context (a :: existing)
        ) attestations;

        (* Process each trace *)
        let all_results = Hashtbl.fold (fun trace_ctx trace_attestations acc ->
          (* Extract trace_id from "trace:{id}" context *)
          let trace_id = if String.length trace_ctx > 6 && String.sub trace_ctx 0 6 = "trace:"
            then String.sub trace_ctx 6 (String.length trace_ctx - 6)
            else trace_ctx in
          let results = process_trace trace_id trace_attestations in
          results @ acc
        ) trace_table [] in

        (* Filter to only emitted weaves *)
        let emitted = List.filter (fun (r : Stitcher.stitch_result) -> r.emitted <> None) all_results in
        Printf.printf "[loom] ATS reader: %d weave(s) from %d trace(s)\n%!"
          (List.length emitted) (Hashtbl.length trace_table);
        Lwt.return emitted
      )
    | Ok resp ->
      Printf.eprintf "[loom] ATS reader: query failed: %s\n%!" resp.error;
      Lwt.return []
    | Error e ->
      let msg = Ocaml_protoc_plugin.Result.show_error e in
      Printf.eprintf "[loom] ATS reader: proto decode error: %s\n%!" msg;
      Lwt.return []
