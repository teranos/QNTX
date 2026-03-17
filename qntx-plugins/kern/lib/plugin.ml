(* kern plugin — Ax query parsing via menhir + sedlex.
 * Uses qntx-plugin shared library for gRPC boilerplate. *)

open Qntx_plugin_proto.Domain

let name = "kern"
let version = Version.value

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

(* --- RPC Handler --- *)

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
       let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ParseAxQueryResponse.to_proto resp) in
       Lwt.return (Grpc.Status.(v OK), Some encoded)
     | Error msg ->
       let resp = Protocol.ParseAxQueryResponse.make
         ~error:msg
         () in
       let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ParseAxQueryResponse.to_proto resp) in
       Lwt.return (Grpc.Status.(v OK), Some encoded))
  | Error e ->
    let msg = Ocaml_protoc_plugin.Result.show_error e in
    let resp = Protocol.ParseAxQueryResponse.make
      ~error:(Printf.sprintf "failed to decode ParseAxQueryRequest: %s" msg)
      () in
    let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ParseAxQueryResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

(* --- Service --- *)

let build_service () =
  Grpc_lwt.Server.Service.(
    v ()
    |> add_rpc ~name:"Metadata"
         ~rpc:(Unary (Qntx_plugin.Server.handle_metadata ~name ~version
                        ~description:"OCaml Ax query parser (menhir + sedlex)"))
    |> add_rpc ~name:"Initialize"
         ~rpc:(Unary (Qntx_plugin.Server.handle_initialize ~name ()))
    |> add_rpc ~name:"Health"
         ~rpc:(Unary Qntx_plugin.Server.handle_health)
    |> add_rpc ~name:"ParseAxQuery"
         ~rpc:(Unary handle_parse_ax_query)
    |> add_rpc ~name:"ConfigSchema"
         ~rpc:(Unary Qntx_plugin.Server.handle_config_schema)
    |> add_rpc ~name:"Shutdown"
         ~rpc:(Unary (Qntx_plugin.Server.handle_shutdown ~name ()))
  )
