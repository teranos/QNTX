(* DomainPluginService implementation for dreamweave
 *
 * gRPC server for the QNTX plugin protocol + HTTP API for the frontend.
 * Non-gRPC requests on the same port are routed to the HTTP handler,
 * so the frontend can talk to dreamweave directly. *)

open Qntx_dreamweave_proto.Domain

let name = "dreamweave"
let version = Version.value

let ats_endpoint = ref ""

let proto_to_string writer =
  Ocaml_protoc_plugin.Writer.contents writer

(* --- gRPC Handlers (QNTX plugin protocol) --- *)

let handle_metadata _raw =
  let resp = Protocol.MetadataResponse.make
    ~name
    ~version
    ~description:"Weave explorer — browse conversation history behind branches"
    () in
  let encoded = proto_to_string (Protocol.MetadataResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_health _raw =
  let resp = Protocol.HealthResponse.make
    ~healthy:true
    ~message:"ok"
    () in
  let encoded = proto_to_string (Protocol.HealthResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_initialize raw =
  let reader = Ocaml_protoc_plugin.Reader.create raw in
  (match Protocol.InitializeRequest.from_proto reader with
   | Ok req ->
     ats_endpoint := req.ats_store_endpoint;
     Printf.printf "[dreamweave] Initialized with ATS endpoint: %s\n%!" req.ats_store_endpoint;
     Ats_client.configure
       ~ats_endpoint:req.ats_store_endpoint
       ~token:req.auth_token
   | Error _ ->
     Printf.printf "[dreamweave] Warning: could not decode InitializeRequest\n%!");
  let resp = Protocol.InitializeResponse.make
    ~handler_names:[]
    () in
  let encoded = proto_to_string (Protocol.InitializeResponse.to_proto resp) in
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
    let encoded = proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)
  | Error e ->
    let msg = Ocaml_protoc_plugin.Result.show_error e in
    let resp = Protocol.ExecuteJobResponse.make
      ~success:false
      ~error:(Printf.sprintf "failed to decode ExecuteJobRequest: %s" msg)
      ~plugin_version:version
      () in
    let encoded = proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_config_schema _raw =
  let resp = Protocol.ConfigSchemaResponse.make () in
  let encoded = proto_to_string (Protocol.ConfigSchemaResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_shutdown _raw =
  Printf.printf "[dreamweave] Shutting down\n%!";
  let encoded = proto_to_string (Protocol.Empty.to_proto (Protocol.Empty.make ())) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

(* --- gRPC service --- *)

let build_service () =
  Grpc_lwt.Server.Service.(
    v ()
    |> add_rpc ~name:"Metadata" ~rpc:(Unary handle_metadata)
    |> add_rpc ~name:"Initialize" ~rpc:(Unary handle_initialize)
    |> add_rpc ~name:"Health" ~rpc:(Unary handle_health)
    |> add_rpc ~name:"ExecuteJob" ~rpc:(Unary handle_execute_job)
    |> add_rpc ~name:"ConfigSchema" ~rpc:(Unary handle_config_schema)
    |> add_rpc ~name:"Shutdown" ~rpc:(Unary handle_shutdown)
  )

let service = lazy (build_service ())

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
    (* Query param: ?name=tmp3/QNTX:main *)
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
    (* CORS preflight *)
    respond_json reqd `OK "{}"
  | `GET, "/" ->
    respond_json reqd `OK
      (Printf.sprintf {|{"name":"dreamweave","version":"%s"}|} version)
  | _ ->
    respond_json reqd `Not_found {|{"error":"not found"}|}

(* --- HTTP/2 server --- *)

let http_port = 5178

let serve port =
  let grpc_handler _addr reqd =
    let request = H2.Reqd.request reqd in
    match request.meth with
    | `POST ->
      (match H2.Headers.get request.headers "content-type" with
       | Some s when String.length s >= 16 && String.sub s 0 16 = "application/grpc" ->
         Grpc_lwt.Server.Service.handle_request (Lazy.force service) reqd
       | _ ->
         H2.Reqd.respond_with_string reqd (H2.Response.create `Not_found) "")
    | _ ->
      H2.Reqd.respond_with_string reqd (H2.Response.create `Not_found) ""
  in
  let http_handler _addr reqd =
    handle_http reqd
  in
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
  let grpc_connection =
    H2_lwt_unix.Server.create_connection_handler
      ~request_handler:grpc_handler
      ~error_handler
  in
  let http_connection =
    H2_lwt_unix.Server.create_connection_handler
      ~request_handler:http_handler
      ~error_handler
  in
  let max_attempts = 10 in
  let open Lwt.Syntax in
  let rec try_bind attempt current_port =
    if attempt >= max_attempts then
      Lwt.fail_with (Printf.sprintf "failed to bind after %d attempts (ports %d-%d)"
        max_attempts port (port + max_attempts - 1))
    else
      let listen_addr = Unix.(ADDR_INET (inet_addr_loopback, current_port)) in
      Lwt.catch
        (fun () ->
          let* _server =
            Lwt_io.establish_server_with_client_socket listen_addr grpc_connection
          in
          Lwt.return current_port)
        (fun _exn ->
          Printf.eprintf "[dreamweave] Port %d in use, trying %d\n%!" current_port (current_port + 1);
          try_bind (attempt + 1) (current_port + 1))
  in
  let* actual_port = try_bind 0 port in
  Printf.printf "QNTX_PLUGIN_PORT=%d\n%!" actual_port;
  Printf.printf "[dreamweave] gRPC server listening on port %d\n%!" actual_port;
  (* Start fixed HTTP API server for the frontend *)
  let http_addr = Unix.(ADDR_INET (inet_addr_loopback, http_port)) in
  let* _http_server =
    Lwt.catch
      (fun () ->
        let* s = Lwt_io.establish_server_with_client_socket http_addr http_connection in
        Printf.printf "[dreamweave] HTTP API listening on port %d\n%!" http_port;
        Lwt.return s)
      (fun exn ->
        Printf.eprintf "[dreamweave] Failed to bind HTTP port %d: %s\n%!" http_port (Printexc.to_string exn);
        Lwt.fail exn)
  in
  let forever, _ = Lwt.wait () in
  forever
