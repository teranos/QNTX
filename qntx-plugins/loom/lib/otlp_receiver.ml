(* OTLP receiver — accepts OpenTelemetry trace exports over HTTP/JSON
 *
 * Third ingestion path alongside UDP (Graunde) and JSONL (historical).
 * Receives OTLP/HTTP JSON-encoded ExportTraceServiceRequest on port 4318,
 * walks the span tree, extracts conversational turns, and feeds them to
 * the stitcher for chunking into weaves.
 *
 * Target producer: Agno framework with OpenTelemetry tracing enabled.
 * Agno emits hierarchical spans for agent runs, LLM calls, and tool
 * executions that map naturally to loom's turn model.
 *
 * Span → turn mapping:
 *   invoke_agent / agent.run  → [session] boundary
 *   gen_ai chat completion     → [assistant] (model response)
 *   tool.*                     → [tool] / [read] / [edit] / [search]
 *   user messages (from input) → [human]
 *)

let otlp_port = 4318

(* --- OTLP JSON attribute extraction --- *)

(* OTLP attributes are arrays of {key, value} where value is typed:
 *   {"key": "foo", "value": {"stringValue": "bar"}}
 *   {"key": "n",   "value": {"intValue": "42"}}
 *)
let find_attr key attrs =
  List.find_map (fun attr ->
    match attr with
    | `Assoc fields ->
      (match List.assoc_opt "key" fields with
       | Some (`String k) when k = key ->
         (match List.assoc_opt "value" fields with
          | Some (`Assoc vfields) ->
            (* Try each OTLP value type *)
            (match List.assoc_opt "stringValue" vfields with
             | Some (`String s) -> Some s
             | _ ->
               match List.assoc_opt "intValue" vfields with
               | Some (`String s) -> Some s  (* OTLP encodes ints as strings *)
               | _ ->
                 match List.assoc_opt "boolValue" vfields with
                 | Some (`Bool b) -> Some (string_of_bool b)
                 | _ -> None)
          | _ -> None)
       | _ -> None)
    | _ -> None
  ) attrs

(* Extract all attributes as (key, string_value) pairs *)
let extract_attrs json =
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "attributes" fields with
     | Some (`List attrs) ->
       List.filter_map (fun attr ->
         match attr with
         | `Assoc afields ->
           (match List.assoc_opt "key" afields with
            | Some (`String k) ->
              (match find_attr k [attr] with
               | Some v -> Some (k, v)
               | None -> None)
            | _ -> None)
         | _ -> None
       ) attrs
     | _ -> [])
  | _ -> []

(* --- Span parsing --- *)

type parsed_span = {
  trace_id : string;
  span_id : string;
  parent_span_id : string;
  name : string;
  start_time_ns : Int64.t;
  attrs : (string * string) list;
  events : Yojson.Safe.t list;
}

let parse_span span_json =
  match span_json with
  | `Assoc fields ->
    let str key = match List.assoc_opt key fields with
      | Some (`String s) -> s | _ -> "" in
    let start_ns = match List.assoc_opt "startTimeUnixNano" fields with
      | Some (`String s) -> (try Int64.of_string s with _ -> 0L)
      | _ -> 0L in
    let events = match List.assoc_opt "events" fields with
      | Some (`List evts) -> evts | _ -> [] in
    Some {
      trace_id = str "traceId";
      span_id = str "spanId";
      parent_span_id = str "parentSpanId";
      name = str "name";
      start_time_ns = start_ns;
      attrs = extract_attrs span_json;
      events;
    }
  | _ -> None

(* --- Span → turn conversion --- *)

(* Derive the label and text for a span based on its name and attributes.
 * Returns None if the span has no weave-worthy content. *)
let span_to_turn span =
  let op_name = match List.assoc_opt "gen_ai.operation.name" span.attrs with
    | Some n -> n | None -> "" in
  let agent_name = match List.assoc_opt "gen_ai.agent.name" span.attrs with
    | Some n -> n | None -> "" in

  (* Agent invocation → session boundary *)
  if op_name = "invoke_agent" || op_name = "create_agent" then
    let name = if agent_name <> "" then agent_name else span.name in
    Some ("session", Printf.sprintf "Agent: %s" name, [])

  (* Chat/completion → extract user input and model output from events *)
  else if op_name = "chat" || op_name = "text_completion" then
    let model = match List.assoc_opt "gen_ai.request.model" span.attrs with
      | Some m -> m | None -> "unknown" in
    (* Look for gen_ai.content.prompt and gen_ai.content.completion events *)
    let prompt_text = List.find_map (fun evt ->
      match evt with
      | `Assoc efields ->
        (match List.assoc_opt "name" efields with
         | Some (`String "gen_ai.content.prompt") ->
           let evt_attrs = match List.assoc_opt "attributes" efields with
             | Some (`List a) -> a | _ -> [] in
           find_attr "gen_ai.prompt" evt_attrs
         | _ -> None)
      | _ -> None
    ) span.events in
    let completion_text = List.find_map (fun evt ->
      match evt with
      | `Assoc efields ->
        (match List.assoc_opt "name" efields with
         | Some (`String "gen_ai.content.completion") ->
           let evt_attrs = match List.assoc_opt "attributes" efields with
             | Some (`List a) -> a | _ -> [] in
           find_attr "gen_ai.completion" evt_attrs
         | _ -> None)
      | _ -> None
    ) span.events in
    (* Also check span-level attributes for input/output messages *)
    let input_msgs = match List.assoc_opt "gen_ai.input.messages" span.attrs with
      | Some m -> m | None -> "" in
    let output_msgs = match List.assoc_opt "gen_ai.output.messages" span.attrs with
      | Some m -> m | None -> "" in
    let text = match completion_text with
      | Some t -> Printf.sprintf "[%s] %s" model t
      | None ->
        if output_msgs <> "" then Printf.sprintf "[%s] %s" model output_msgs
        else Printf.sprintf "[%s] (completion)" model
    in
    (* If we have user input, emit it as a separate human turn first
     * by returning a special marker. The caller handles this. *)
    let human_text = match prompt_text with
      | Some t -> t
      | None -> if input_msgs <> "" then input_msgs else "" in
    if human_text <> "" then
      (* Return assistant turn; human turn handled separately *)
      Some ("assistant", text, [("_human_text", human_text)])
    else
      Some ("assistant", text, [])

  (* Tool execution spans *)
  else if String.length span.name > 5 && String.sub span.name 0 5 = "tool." then
    let tool_name = String.sub span.name 5 (String.length span.name - 5) in
    let input_text = match List.assoc_opt "gen_ai.tool.input" span.attrs with
      | Some t -> t | None -> tool_name in
    (* Map tool names to loom labels *)
    let label, display, paths = match tool_name with
      | "read_file" | "read" ->
        let path = match List.assoc_opt "gen_ai.tool.input" span.attrs with
          | Some p -> p | None -> tool_name in
        let tail = Stitcher.file_tail path in
        ("read", tail, [(tail, path)])
      | "edit_file" | "edit" | "write_file" | "write" ->
        let path = match List.assoc_opt "gen_ai.tool.input" span.attrs with
          | Some p -> p | None -> tool_name in
        let tail = Stitcher.file_tail path in
        ("edit", tail, [(tail, path)])
      | "search" | "grep" | "glob" | "find" ->
        ("search", input_text, [])
      | "bash" | "shell" | "run_command" ->
        if Stitcher.is_weave_worthy_command input_text then
          ("tool", input_text, [])
        else
          ("tool", tool_name, [])  (* still record tool name *)
      | _ ->
        ("tool", Printf.sprintf "%s: %s" tool_name input_text, [])
    in
    Some (label, display, paths)

  (* Generic spans with gen_ai attributes — try to extract something useful *)
  else if agent_name <> "" then
    Some ("agent", Printf.sprintf "%s: %s" agent_name span.name, [])

  (* Unknown spans — skip *)
  else
    None

(* --- Trace processing --- *)

(* Sort spans by start time and convert to stitcher turns *)
let process_trace ~resource_attrs spans =
  (* Sort by start_time_ns ascending *)
  let sorted = List.sort (fun a b ->
    Int64.compare a.start_time_ns b.start_time_ns
  ) spans in

  (* Derive branch from resource attributes *)
  let service_name = match find_attr "service.name" resource_attrs with
    | Some n -> n | None -> "" in
  let project = match find_attr "qntx.project" resource_attrs with
    | Some p -> p | None -> "" in
  let branch_prefix = if project <> "" then project
    else if service_name <> "" then service_name
    else "agno" in

  (* Find the root agent span for context *)
  let root_agent = List.find_opt (fun s ->
    match List.assoc_opt "gen_ai.operation.name" s.attrs with
    | Some "invoke_agent" -> true | _ -> false
  ) sorted in
  let agent_name = match root_agent with
    | Some s ->
      (match List.assoc_opt "gen_ai.agent.name" s.attrs with
       | Some n -> n | None -> "agent")
    | None -> "agent" in
  let branch = Printf.sprintf "%s:%s" branch_prefix agent_name in

  (* Use trace_id as session context *)
  let trace_id = match sorted with
    | s :: _ -> s.trace_id
    | [] -> "unknown" in
  let context = "trace:" ^ trace_id in

  (* Convert sorted spans to turns *)
  let all_results = ref [] in

  (* Emit SessionStart *)
  let start_results = Stitcher.stitch_turn
    ~branch ~context ~predicate:"SessionStart"
    ~label:"session" ~text:(Printf.sprintf "Agent: %s (trace %s)" agent_name trace_id)
    ~paths:[] () in
  all_results := start_results @ !all_results;

  List.iter (fun span ->
    match span_to_turn span with
    | None -> ()
    | Some (label, text, meta) ->
      let timestamp = Int64.to_int (Int64.div span.start_time_ns 1_000_000L) in
      (* If this is a chat span with human input, emit human turn first *)
      let human_text = List.assoc_opt "_human_text" meta in
      (match human_text with
       | Some ht when ht <> "" ->
         let human_results = Stitcher.stitch_turn
           ~branch ~context ~predicate:"UserPromptSubmit"
           ~label:"human" ~text:ht ~paths:[] ~timestamp () in
         all_results := human_results @ !all_results
       | _ -> ());
      (* Filter out the _human_text meta from paths *)
      let paths = List.filter_map (fun (k, v) ->
        if k = "_human_text" then None else Some (k, v)
      ) meta in
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
    ~label:"session" ~text:(Printf.sprintf "Agent completed: %s" agent_name)
    ~paths:[] () in
  all_results := end_results @ !all_results;

  List.rev !all_results

(* --- OTLP JSON decoding --- *)

(* Decode ExportTraceServiceRequest JSON and process all traces *)
let decode_and_process body =
  let json = try Some (Yojson.Safe.from_string body)
    with Yojson.Json_error msg ->
      Printf.eprintf "[loom] OTLP JSON parse error: %s\n%!" msg;
      None
  in
  match json with
  | None -> []
  | Some json ->
    let resource_spans = match json with
      | `Assoc fields ->
        (match List.assoc_opt "resourceSpans" fields with
         | Some (`List rs) -> rs | _ -> [])
      | _ -> []
    in
    List.concat_map (fun rs_json ->
      let resource_attrs = match rs_json with
        | `Assoc fields ->
          (match List.assoc_opt "resource" fields with
           | Some (`Assoc rfields) ->
             (match List.assoc_opt "attributes" rfields with
              | Some (`List attrs) -> attrs | _ -> [])
           | _ -> [])
        | _ -> []
      in
      let scope_spans = match rs_json with
        | `Assoc fields ->
          (match List.assoc_opt "scopeSpans" fields with
           | Some (`List ss) -> ss | _ -> [])
        | _ -> []
      in
      let spans = List.concat_map (fun ss_json ->
        match ss_json with
        | `Assoc fields ->
          (match List.assoc_opt "spans" fields with
           | Some (`List span_list) ->
             List.filter_map parse_span span_list
           | _ -> [])
        | _ -> []
      ) scope_spans in
      (* Group spans by trace_id *)
      let trace_table : (string, parsed_span list) Hashtbl.t = Hashtbl.create 8 in
      List.iter (fun span ->
        let existing = match Hashtbl.find_opt trace_table span.trace_id with
          | Some l -> l | None -> [] in
        Hashtbl.replace trace_table span.trace_id (span :: existing)
      ) spans;
      (* Process each trace *)
      Hashtbl.fold (fun _trace_id trace_spans acc ->
        process_trace ~resource_attrs trace_spans @ acc
      ) trace_table []
    ) resource_spans

(* --- HTTP/1.1 server for OTLP --- *)

(* OTLP/HTTP uses HTTP/1.1 with POST /v1/traces.
 * We use a simple Lwt TCP server with basic HTTP parsing.
 * No need for h2 here — OTLP/HTTP is plain HTTP/1.1. *)

let read_http_request ic =
  let open Lwt.Syntax in
  (* Read request line *)
  let* request_line = Lwt_io.read_line ic in
  (* Read headers *)
  let headers = Hashtbl.create 8 in
  let rec read_headers () =
    let* line = Lwt_io.read_line ic in
    if String.length line = 0 || line = "\r" then
      Lwt.return_unit
    else (
      (* Strip trailing \r if present *)
      let line = if String.length line > 0 && line.[String.length line - 1] = '\r'
        then String.sub line 0 (String.length line - 1) else line in
      (match String.index_opt line ':' with
       | Some i ->
         let key = String.lowercase_ascii (String.sub line 0 i) in
         let value = String.trim (String.sub line (i + 1) (String.length line - i - 1)) in
         Hashtbl.replace headers key value
       | None -> ());
      read_headers ())
  in
  let* () = read_headers () in
  (* Read body based on content-length *)
  let content_length = match Hashtbl.find_opt headers "content-length" with
    | Some s -> (try int_of_string s with _ -> 0)
    | None -> 0 in
  let* body = if content_length > 0 then (
    let buf = Bytes.create content_length in
    let* () = Lwt_io.read_into_exactly ic buf 0 content_length in
    Lwt.return (Bytes.to_string buf)
  ) else Lwt.return "" in
  Lwt.return (request_line, body)

let write_http_response oc status_code status_text body =
  let response = Printf.sprintf
    "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nAccess-Control-Allow-Origin: *\r\nAccess-Control-Allow-Methods: POST, OPTIONS\r\nAccess-Control-Allow-Headers: content-type\r\nConnection: close\r\n\r\n%s"
    status_code status_text (String.length body) body in
  Lwt_io.write oc response

let persist_results results =
  let open Lwt.Syntax in
  Lwt_list.iter_p (fun (result : Stitcher.stitch_result) ->
    match result.emitted with
    | Some block ->
      let* ats_result = Ats_client.create_weave
        ~branch:result.branch
        ~context:result.context
        ~text:block
        ~word_count:(Stitcher.word_count block)
        ~turn_count:result.turn_count
        ~paths:result.paths
        ~original_timestamp:result.timestamp
        ~weave_source:"agno-otel"
        ()
      in
      (match ats_result with
       | Ok () -> ()
       | Error msg ->
         Printf.eprintf "[loom] Failed to persist OTLP weave for %s: %s\n%!" result.branch msg);
      Lwt.return_unit
    | None -> Lwt.return_unit
  ) results

let handle_connection (ic, oc) =
  let open Lwt.Syntax in
  Lwt.catch
    (fun () ->
      let* (request_line, body) = read_http_request ic in
      (* Parse method and path from request line: "POST /v1/traces HTTP/1.1" *)
      let parts = String.split_on_char ' ' request_line in
      let method_ = match parts with m :: _ -> m | [] -> "" in
      let path = match parts with _ :: p :: _ -> p | _ -> "" in
      (* Strip trailing \r from path *)
      let path = if String.length path > 0 && path.[String.length path - 1] = '\r'
        then String.sub path 0 (String.length path - 1) else path in
      match method_, path with
      | "POST", "/v1/traces" ->
        Printf.printf "[loom] OTLP trace export received (%d bytes)\n%!" (String.length body);
        let results = decode_and_process body in
        let emitted = List.filter (fun (r : Stitcher.stitch_result) -> r.emitted <> None) results in
        let* () = persist_results emitted in
        let weave_count = List.length emitted in
        Printf.printf "[loom] OTLP processed: %d weave(s) emitted\n%!" weave_count;
        (* OTLP expects ExportTraceServiceResponse — empty JSON object on success *)
        write_http_response oc 200 "OK"
          (Printf.sprintf {|{"partialSuccess":null}|})
      | "OPTIONS", _ ->
        write_http_response oc 200 "OK" ""
      | "GET", "/" ->
        write_http_response oc 200 "OK"
          (Printf.sprintf {|{"name":"loom-otlp","port":%d}|} otlp_port)
      | _ ->
        write_http_response oc 404 "Not Found" {|{"error":"not found"}|})
    (fun exn ->
      Printf.eprintf "[loom] OTLP connection error: %s\n%!" (Printexc.to_string exn);
      Lwt.return_unit)

let start () =
  let open Lwt.Syntax in
  let addr = Unix.(ADDR_INET (inet_addr_loopback, otlp_port)) in
  let* server = Lwt_io.establish_server_with_client_socket addr
    (fun _addr socket ->
      Lwt.async (fun () ->
        let ic = Lwt_io.of_fd ~mode:Lwt_io.Input socket in
        let oc = Lwt_io.of_fd ~mode:Lwt_io.Output socket in
        let* () = handle_connection (ic, oc) in
        let* () = Lwt_io.close oc in
        Lwt.return_unit))
  in
  ignore server;
  Printf.printf "[loom] OTLP receiver on port %d (POST /v1/traces)\n%!" otlp_port;
  Lwt.return_unit
