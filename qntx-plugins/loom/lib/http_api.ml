(* HTTP API — serves weave data to the Svelte frontend
 *
 * Runs alongside loom's gRPC server on a separate port.
 * Endpoints query ATS for weave attestations and return JSON. *)

let respond_json reqd status json_str =
  let headers = H2.Headers.of_list [
    ("content-type", "application/json");
    ("access-control-allow-origin", "*");
    ("access-control-allow-methods", "GET, OPTIONS");
    ("access-control-allow-headers", "content-type");
  ] in
  let response = H2.Response.create ~headers status in
  H2.Reqd.respond_with_string reqd response json_str

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
