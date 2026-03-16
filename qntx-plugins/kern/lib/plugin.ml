(* DomainPluginService implementation for kern
 *
 * Implements the gRPC contract from domain.proto. Only ParseAxQuery does
 * real work — the rest are stubs required by the shared plugin interface.
 *
 * Same pure-OCaml gRPC stack as loom: grpc-lwt on h2, no C FFI. *)

open Kern_proto.Domain

let name = "kern"
let version = Version.value

(* --- Serialization helpers --- *)

let proto_to_string writer =
  Ocaml_protoc_plugin.Writer.contents writer

(* --- Parse logic --- *)

let parse_query input =
  let buf = Sedlexing.Utf8.from_string input in
  let lexbuf = Lexing.from_string "" in
  let tokenizer = Sedlexing.with_tokenizer Lexer.token buf in
  let lexer _lexbuf =
    let token, start_pos, end_pos = tokenizer () in
    lexbuf.lex_start_p <- start_pos;
    lexbuf.lex_curr_p <- end_pos;
    token
  in
  match Parser.query lexer lexbuf with
  | query ->
    let json = Ast.to_json query in
    Ok (Yojson.Safe.to_string json)
  | exception Parser.Error ->
    Error "parse error"

(* --- RPC Handlers --- *)

let handle_metadata _raw =
  let resp = Protocol.MetadataResponse.make
    ~name
    ~version
    ~description:"OCaml Ax query parser (menhir + sedlex)"
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
   | Ok _req ->
     Printf.printf "[kern] Initialized\n%!"
   | Error _ ->
     Printf.printf "[kern] Warning: could not decode InitializeRequest\n%!");
  let resp = Protocol.InitializeResponse.make () in
  let encoded = proto_to_string (Protocol.InitializeResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_parse_ax_query raw =
  let reader = Ocaml_protoc_plugin.Reader.create raw in
  match Protocol.ParseAxQueryRequest.from_proto reader with
  | Ok query_str ->
    Printf.printf "[kern] ParseAxQuery: %s\n%!" query_str;
    (match parse_query query_str with
     | Ok json ->
       let resp = Protocol.ParseAxQueryResponse.make
         ~result:(Bytes.of_string json)
         () in
       let encoded = proto_to_string (Protocol.ParseAxQueryResponse.to_proto resp) in
       Lwt.return (Grpc.Status.(v OK), Some encoded)
     | Error msg ->
       let resp = Protocol.ParseAxQueryResponse.make
         ~error:msg
         () in
       let encoded = proto_to_string (Protocol.ParseAxQueryResponse.to_proto resp) in
       Lwt.return (Grpc.Status.(v OK), Some encoded))
  | Error e ->
    let msg = Ocaml_protoc_plugin.Result.show_error e in
    let resp = Protocol.ParseAxQueryResponse.make
      ~error:(Printf.sprintf "failed to decode ParseAxQueryRequest: %s" msg)
      () in
    let encoded = proto_to_string (Protocol.ParseAxQueryResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_config_schema _raw =
  let resp = Protocol.ConfigSchemaResponse.make () in
  let encoded = proto_to_string (Protocol.ConfigSchemaResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_shutdown _raw =
  Printf.printf "[kern] Shutting down\n%!";
  let encoded = proto_to_string (Protocol.Empty.to_proto (Protocol.Empty.make ())) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

(* --- gRPC service wiring --- *)

let build_service () =
  Grpc_lwt.Server.Service.(
    v ()
    |> add_rpc ~name:"Metadata" ~rpc:(Unary handle_metadata)
    |> add_rpc ~name:"Initialize" ~rpc:(Unary handle_initialize)
    |> add_rpc ~name:"Health" ~rpc:(Unary handle_health)
    |> add_rpc ~name:"ParseAxQuery" ~rpc:(Unary handle_parse_ax_query)
    |> add_rpc ~name:"ConfigSchema" ~rpc:(Unary handle_config_schema)
    |> add_rpc ~name:"Shutdown" ~rpc:(Unary handle_shutdown)
  )

(* --- Custom request handler --- *)

(* Bypass grpc-lwt's encoding negotiation bug (same as loom). *)

let service = lazy (build_service ())

let route_request reqd =
  let request = H2.Reqd.request reqd in
  let respond_with code =
    H2.Reqd.respond_with_string reqd (H2.Response.create code) ""
  in
  match request.meth with
  | `POST ->
    (match H2.Headers.get request.headers "content-type" with
     | Some s when String.length s >= 16 && String.sub s 0 16 = "application/grpc" ->
       Grpc_lwt.Server.Service.handle_request (Lazy.force service) reqd
     | _ ->
       respond_with `Unsupported_media_type)
  | _ ->
    respond_with `Not_found

(* --- HTTP/2 server --- *)

let serve port =
  let request_handler _addr reqd =
    route_request reqd
  in
  let error_handler _addr ?request:_ error respond =
    let msg = match error with
      | `Exn exn -> Printexc.to_string exn
      | `Bad_gateway -> "Bad gateway"
      | `Bad_request -> "Bad request"
      | `Internal_server_error -> "Internal server error"
    in
    Printf.eprintf "[kern] HTTP/2 error: %s\n%!" msg;
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
          Printf.eprintf "[kern] Port %d in use, trying %d\n%!" current_port (current_port + 1);
          try_bind (attempt + 1) (current_port + 1))
  in
  let* actual_port = try_bind 0 port in
  Printf.printf "QNTX_PLUGIN_PORT=%d\n%!" actual_port;
  Printf.printf "[kern] gRPC server listening on port %d\n%!" actual_port;
  let forever, _ = Lwt.wait () in
  forever
