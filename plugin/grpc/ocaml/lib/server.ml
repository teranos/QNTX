(* Shared gRPC plugin server for QNTX OCaml plugins.
 *
 * Provides the H2/gRPC boilerplate that all OCaml plugins share:
 * port auto-increment, content-type routing, QNTX_PLUGIN_PORT announcement,
 * and default RPC handlers for Metadata, Health, ConfigSchema, Shutdown. *)

open Qntx_plugin_proto.Domain

(* --- Serialization helper --- *)

let proto_to_string writer =
  Ocaml_protoc_plugin.Writer.contents writer

(* --- Default RPC handlers --- *)

let handle_metadata ~name ~version ~description _raw =
  let resp = Protocol.MetadataResponse.make
    ~name ~version ~description () in
  let encoded = proto_to_string (Protocol.MetadataResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_health _raw =
  let resp = Protocol.HealthResponse.make
    ~healthy:true ~message:"ok" () in
  let encoded = proto_to_string (Protocol.HealthResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_config_schema _raw =
  let resp = Protocol.ConfigSchemaResponse.make () in
  let encoded = proto_to_string (Protocol.ConfigSchemaResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_shutdown ?(on_shutdown = fun () -> Lwt.return_unit) ~name () =
  fun _raw ->
    let open Lwt.Syntax in
    Printf.printf "[%s] Shutting down\n%!" name;
    let* () = on_shutdown () in
    let encoded = proto_to_string (Protocol.Empty.to_proto (Protocol.Empty.make ())) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_initialize ?(on_init = fun _req -> ()) ~name () =
  fun raw ->
    let reader = Ocaml_protoc_plugin.Reader.create raw in
    (match Protocol.InitializeRequest.from_proto reader with
     | Ok req ->
       Printf.printf "[%s] Initialized\n%!" name;
       on_init req
     | Error _ ->
       Printf.printf "[%s] Warning: could not decode InitializeRequest\n%!" name);
    let resp = Protocol.InitializeResponse.make () in
    let encoded = proto_to_string (Protocol.InitializeResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

(* --- Content-type routing --- *)

let route_grpc service reqd =
  let request = H2.Reqd.request reqd in
  let respond_with code =
    H2.Reqd.respond_with_string reqd (H2.Response.create code) ""
  in
  match request.meth with
  | `POST ->
    (match H2.Headers.get request.headers "content-type" with
     | Some s when String.length s >= 16 && String.sub s 0 16 = "application/grpc" ->
       Grpc_lwt.Server.Service.handle_request service reqd
     | _ ->
       respond_with `Unsupported_media_type)
  | _ ->
    respond_with `Not_found

(* --- H2 server with port auto-increment --- *)

let serve ~name ~service port =
  let lazy_service = lazy service in
  let request_handler _addr reqd =
    route_grpc (Lazy.force lazy_service) reqd
  in
  let error_handler _addr ?request:_ error respond =
    let msg = match error with
      | `Exn exn -> Printexc.to_string exn
      | `Bad_gateway -> "Bad gateway"
      | `Bad_request -> "Bad request"
      | `Internal_server_error -> "Internal server error"
    in
    Printf.eprintf "[%s] HTTP/2 error: %s\n%!" name msg;
    let body = respond H2.Headers.empty in
    H2.Body.Writer.close body
  in
  let connection_handler =
    H2_lwt_unix.Server.create_connection_handler
      ~request_handler
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
            Lwt_io.establish_server_with_client_socket listen_addr connection_handler
          in
          Lwt.return current_port)
        (fun _exn ->
          Printf.eprintf "[%s] Port %d in use, trying %d\n%!" name current_port (current_port + 1);
          try_bind (attempt + 1) (current_port + 1))
  in
  let* actual_port = try_bind 0 port in
  Printf.printf "QNTX_PLUGIN_PORT=%d\n%!" actual_port;
  Printf.printf "[%s] gRPC server listening on port %d\n%!" name actual_port;
  let forever, _ = Lwt.wait () in
  forever

(* --- Main entrypoint --- *)

let run ~name ~build_service () =
  let port = ref 0 in
  let spec = [
    ("--port", Arg.Set_int port, "gRPC listen port (assigned by QNTX)");
  ] in
  Arg.parse spec (fun _ -> ()) name;

  if !port = 0 then (
    Printf.eprintf "Error: --port is required\n";
    exit 1
  );

  Printf.printf "[%s] Starting on port %d\n%!" name !port;

  Lwt_main.run (
    let open Lwt.Syntax in
    let sigterm_waiter, sigterm_wakener = Lwt.wait () in
    let _sigterm_handler = Lwt_unix.on_signal Sys.sigterm (fun _signum ->
      Lwt.wakeup sigterm_wakener ()
    ) in
    let service = build_service () in
    let srv = serve ~name ~service !port in
    let shutdown =
      let* () = sigterm_waiter in
      Printf.printf "[%s] SIGTERM received — shutting down\n%!" name;
      Lwt.return_unit
    in
    Lwt.pick [srv; shutdown]
  )
