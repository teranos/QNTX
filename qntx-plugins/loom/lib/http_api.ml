(* HTTP API — serves weave data to the Svelte frontend
 *
 * Runs alongside loom's gRPC server on a separate port.
 * Endpoints query ATS for weave attestations and return JSON. *)

let respond_json reqd status json_str =
  let headers = H2.Headers.of_list [
    ("content-type", "application/json");
    ("access-control-allow-origin", "*");
    ("access-control-allow-methods", "GET, POST, OPTIONS");
    ("access-control-allow-headers", "content-type");
  ] in
  let response = H2.Response.create ~headers status in
  H2.Reqd.respond_with_string reqd response json_str

(* Read entire request body from H2 *)
let read_body reqd callback =
  let body = H2.Reqd.request_body reqd in
  let buf = Buffer.create 256 in
  let rec read () =
    H2.Body.Reader.schedule_read body
      ~on_eof:(fun () -> callback (Buffer.contents buf))
      ~on_read:(fun bigstring ~off ~len ->
        let chunk = Bigstringaf.substring bigstring ~off ~len in
        Buffer.add_string buf chunk;
        read ())
  in
  read ()

(* Persist emitted weave blocks to ATS *)
let persist_weaves results =
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
        ()
      in
      (match ats_result with
       | Ok () -> ()
       | Error msg ->
         Printf.eprintf "[loom] Failed to persist imported weave for %s: %s\n%!" result.branch msg);
      Lwt.return_unit
    | None -> Lwt.return_unit
  ) results

let handle_http reqd =
  let request = H2.Reqd.request reqd in
  let path = request.target in
  let open Lwt.Syntax in
  match request.meth, path with
  | `GET, "/api/weaves" ->
    Lwt.async (fun () ->
      let* result = Ats_client.get_weaves () in
      (match result with
       | Ok attestations ->
         let json = Serialize_ui.group_by_branch attestations in
         respond_json reqd `OK (Yojson.Safe.to_string json)
       | Error msg ->
         respond_json reqd `Internal_server_error
           (Printf.sprintf {|{"error":"%s"}|} msg));
      Lwt.return_unit)
  | `GET, "/api/weaves/branch" ->
    let uri = Uri.of_string ("http://localhost" ^ path) in
    let branch = Uri.get_query_param uri "name" in
    Lwt.async (fun () ->
      let subjects = match branch with
        | Some b -> Some [b]
        | None -> None
      in
      let* result = Ats_client.get_weaves ?subjects () in
      (match result with
       | Ok attestations ->
         let json = Serialize_ui.attestations_to_json attestations in
         respond_json reqd `OK (Yojson.Safe.to_string json)
       | Error msg ->
         respond_json reqd `Internal_server_error
           (Printf.sprintf {|{"error":"%s"}|} msg));
      Lwt.return_unit)
  | `GET, "/api/sessions" ->
    Lwt.async (fun () ->
      let* sessions = Session_discovery.discover () in
      let json = Session_discovery.sessions_to_json sessions in
      respond_json reqd `OK (Yojson.Safe.to_string json);
      Lwt.return_unit)
  | `POST, "/api/import" ->
    read_body reqd (fun body ->
      Lwt.async (fun () ->
        let json = try Some (Yojson.Safe.from_string body)
          with _ -> None in
        let file_path = match json with
          | Some (`Assoc fields) ->
            (match List.assoc_opt "file_path" fields with
             | Some (`String p) -> Some p | _ -> None)
          | _ -> None
        in
        match file_path with
        | None ->
          respond_json reqd `Bad_request {|{"error":"missing file_path"}|};
          Lwt.return_unit
        | Some file_path ->
          Printf.printf "[loom] Importing JSONL: %s\n%!" file_path;
          let results = Jsonl_reader.ingest ~file_path ~branch_override:None in
          let weave_count = List.length results in
          let* () = persist_weaves results in
          (* Get file stats for WeaveComplete attestation *)
          let file_size = (Unix.stat file_path).st_size in
          let line_count = let ic = open_in file_path in
            let n = ref 0 in
            (try while true do ignore (input_line ic); incr n done
             with End_of_file -> close_in ic);
            !n in
          (* Extract session ID from filename (uuid.jsonl) *)
          let basename = Filename.basename file_path in
          let session_id = Filename.remove_extension basename in
          let* _wc = Ats_client.create_weave_complete
            ~session_id ~file_path ~file_size ~line_count ~weave_count in
          Printf.printf "[loom] Import complete: %d weaves from %s\n%!" weave_count file_path;
          respond_json reqd `OK
            (Printf.sprintf {|{"success":true,"weaves_created":%d,"file":"%s","session_id":"%s"}|}
               weave_count file_path session_id);
          Lwt.return_unit))
  | `OPTIONS, _ ->
    respond_json reqd `OK "{}"
  | `GET, "/" ->
    respond_json reqd `OK
      (Printf.sprintf {|{"name":"loom","version":"%s"}|} Version.value)
  | _ ->
    respond_json reqd `Not_found {|{"error":"not found"}|}

let http_port = 5178

let start () =
  let error_handler _addr ?request:_ error respond =
    let msg = match error with
      | `Exn exn -> Printexc.to_string exn
      | `Bad_gateway -> "Bad gateway"
      | `Bad_request -> "Bad request"
      | `Internal_server_error -> "Internal server error"
    in
    Printf.eprintf "[loom] HTTP/2 error: %s\n%!" msg;
    let body = respond H2.Headers.empty in
    H2.Body.Writer.close body
  in
  let http_connection =
    H2_lwt_unix.Server.create_connection_handler
      ~request_handler:(fun _addr reqd -> handle_http reqd)
      ~error_handler
  in
  let http_addr = Unix.(ADDR_INET (inet_addr_loopback, http_port)) in
  Lwt.async (fun () ->
    Lwt.catch
      (fun () ->
        let open Lwt.Syntax in
        let* _s = Lwt_io.establish_server_with_client_socket http_addr http_connection in
        Printf.printf "[loom] HTTP API listening on port %d\n%!" http_port;
        Lwt.return_unit)
      (fun exn ->
        Printf.eprintf "[loom] Failed to bind HTTP port %d: %s\n%!" http_port (Printexc.to_string exn);
        Lwt.return_unit))
