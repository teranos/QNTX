(* DomainPluginService implementation for dreamweave
 * Uses qntx-plugin shared library for gRPC boilerplate.
 * Non-gRPC requests on the same port are routed to the HTTP handler,
 * so the frontend can talk to dreamweave directly. *)

open Qntx_plugin_proto.Domain

let name = "dreamweave"
let version = Version.value

(* --- Dreamweave-specific RPC Handlers --- *)

let handle_initialize raw =
  let reader = Ocaml_protoc_plugin.Reader.create raw in
  (match Protocol.InitializeRequest.from_proto reader with
   | Ok req ->
     Printf.printf "[dreamweave] Initialized with ATS endpoint: %s\n%!" req.ats_store_endpoint;
     Ats_client.configure
       ~ats_endpoint:req.ats_store_endpoint
       ~token:req.auth_token
   | Error _ ->
     Printf.printf "[dreamweave] Warning: could not decode InitializeRequest\n%!");
  let resp = Protocol.InitializeResponse.make
    ~handler_names:[]
    () in
  let encoded = Qntx_plugin.Server.proto_to_string (Protocol.InitializeResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_execute_job raw =
  let reader = Ocaml_protoc_plugin.Reader.create raw in
  match Protocol.ExecuteJobRequest.from_proto reader with
  | Ok req ->
    let resp = Protocol.ExecuteJobResponse.make
      ~success:false
      ~error:(Printf.sprintf "dreamweave has no job handlers (got: %s)" req.handler_name)
      ~plugin_version:version
      () in
    let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)
  | Error e ->
    let msg = Ocaml_protoc_plugin.Result.show_error e in
    let resp = Protocol.ExecuteJobResponse.make
      ~success:false
      ~error:(Printf.sprintf "failed to decode ExecuteJobRequest: %s" msg)
      ~plugin_version:version
      () in
    let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

(* --- Service --- *)

let build_service () =
  Grpc_lwt.Server.Service.(
    v ()
    |> add_rpc ~name:"Metadata"
         ~rpc:(Unary (Qntx_plugin.Server.handle_metadata ~name ~version
                        ~description:"Weave explorer — browse conversation history behind branches"))
    |> add_rpc ~name:"Initialize"
         ~rpc:(Unary handle_initialize)
    |> add_rpc ~name:"Health"
         ~rpc:(Unary Qntx_plugin.Server.handle_health)
    |> add_rpc ~name:"ExecuteJob"
         ~rpc:(Unary handle_execute_job)
    |> add_rpc ~name:"ConfigSchema"
         ~rpc:(Unary Qntx_plugin.Server.handle_config_schema)
    |> add_rpc ~name:"Shutdown"
         ~rpc:(Unary (Qntx_plugin.Server.handle_shutdown ~name ()))
  )

(* --- HTTP API (for the frontend) --- *)

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
         let json = Weave.group_by_branch attestations in
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
         let json = Weave.attestations_to_json attestations in
         respond_json reqd `OK (Yojson.Safe.to_string json)
       | Error msg ->
         respond_json reqd `Internal_server_error
           (Printf.sprintf {|{"error":"%s"}|} msg));
      Lwt.return_unit)
  | `OPTIONS, _ ->
    respond_json reqd `OK "{}"
  | `GET, "/" ->
    respond_json reqd `OK
      (Printf.sprintf {|{"name":"dreamweave","version":"%s"}|} version)
  | _ ->
    respond_json reqd `Not_found {|{"error":"not found"}|}

let http_port = 5178

let start_http_server () =
  let error_handler _addr ?request:_ error respond =
    let msg = match error with
      | `Exn exn -> Printexc.to_string exn
      | `Bad_gateway -> "Bad gateway"
      | `Bad_request -> "Bad request"
      | `Internal_server_error -> "Internal server error"
    in
    Printf.eprintf "[dreamweave] HTTP/2 error: %s\n%!" msg;
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
        Printf.printf "[dreamweave] HTTP API listening on port %d\n%!" http_port;
        Lwt.return_unit)
      (fun exn ->
        Printf.eprintf "[dreamweave] Failed to bind HTTP port %d: %s\n%!" http_port (Printexc.to_string exn);
        Lwt.return_unit))
