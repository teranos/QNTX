(* ATSStoreService gRPC client — reads and writes weave attestations
 *
 * Connects to QNTX's ATSStoreService endpoint (received during Initialize).
 * Writes: GenerateAndCreateAttestation to persist woven blocks.
 * Reads: GetAttestations to serve weave data to the frontend. *)

open Qntx_plugin_proto.Atsstore

(* --- Connection state --- *)

let endpoint = ref ""
let auth_token = ref ""

let configure ~ats_endpoint ~token =
  endpoint := ats_endpoint;
  auth_token := token;
  Printf.printf "[ats] ATS client configured: %s\n%!" ats_endpoint

(* --- gRPC call helper --- *)

let grpc_call ~path ~request_bytes =
  let host, port =
    match String.split_on_char ':' !endpoint with
    | [h; p] -> (h, int_of_string_opt p |> Option.value ~default:0)
    | _ -> ("127.0.0.1", 0)
  in
  if port = 0 then (
    Printf.eprintf "[ats] Invalid ATS endpoint: %s\n%!" !endpoint;
    Lwt.return_error "invalid ATS endpoint"
  ) else
    let open Lwt.Syntax in
    Lwt.catch
      (fun () ->
        let addr = Unix.(ADDR_INET (inet_addr_of_string host, port)) in
        let socket = Lwt_unix.socket Unix.PF_INET Unix.SOCK_STREAM 0 in
        let* () = Lwt_unix.connect socket addr in

        let* conn =
          H2_lwt_unix.Client.create_connection
            ~error_handler:(fun _ -> Printf.eprintf "[ats] ATS h2 connection error\n%!")
            socket
        in

        let h2_request =
          H2.Request.create
            ~scheme:"http"
            ~headers:(H2.Headers.of_list [
              ("content-type", "application/grpc+proto");
              ("te", "trailers");
            ])
            `POST
            path
        in

        let response_buf = Buffer.create 4096 in
        let response_received, response_wakeup = Lwt.wait () in

        let response_handler _response body =
          let rec read_loop () =
            H2.Body.Reader.schedule_read body
              ~on_read:(fun bigstring ~off ~len ->
                let chunk = Bigstringaf.substring bigstring ~off ~len in
                Buffer.add_string response_buf chunk;
                read_loop ())
              ~on_eof:(fun () ->
                Lwt.wakeup_later response_wakeup ())
          in
          read_loop ()
        in

        let body_writer =
          H2_lwt_unix.Client.request conn
            ~flush_headers_immediately:true
            h2_request
            ~error_handler:(fun _ -> Printf.eprintf "[ats] ATS request error\n%!")
            ~response_handler
        in
        let grpc_frame = Grpc.Message.make request_bytes in
        H2.Body.Writer.write_string body_writer grpc_frame;
        H2.Body.Writer.close body_writer;

        let* () = response_received in

        let response_data = Buffer.contents response_buf in
        let grpc_buf = Grpc.Buffer.v () in
        let response_bigstring =
          Bigstringaf.of_string ~off:0 ~len:(String.length response_data) response_data
        in
        Grpc.Buffer.copy_from_bigstringaf
          ~src_off:0 ~src:response_bigstring
          ~dst:grpc_buf ~length:(String.length response_data);

        let payload = match Grpc.Message.extract grpc_buf with
          | Some msg -> Ok msg
          | None -> Error (Printf.sprintf "no gRPC message in response (%d bytes)" (String.length response_data))
        in

        let* () = H2_lwt_unix.Client.shutdown conn in
        Lwt.return payload)
      (fun exn ->
        Printf.eprintf "[ats] ATS client error: %s\n%!" (Printexc.to_string exn);
        Lwt.return_error (Printexc.to_string exn))

(* --- Create a weave attestation --- *)

let create_weave ~branch ~context ~text ~word_count ~turn_count ~paths ?(original_timestamp=0) ~weave_source () =
  if !endpoint = "" then (
    Printf.eprintf "[ats] ATS client not configured, dropping weave\n%!";
    Lwt.return_error "ATS client not configured"
  ) else
    let open Lwt.Syntax in
    let open Qntx_plugin_proto.Struct.Google.Protobuf in
    let string_val s = Value.make ~kind:(`String_value s) () in
    let number_val n = Value.make ~kind:(`Number_value (Float.of_int n)) () in
    let paths_fields = List.map (fun (tail, full) ->
      (tail, Some (string_val full))
    ) paths in
    let paths_val =
      Value.make ~kind:(`Struct_value (Struct.make ~fields:paths_fields ())) ()
    in
    let base_fields = [
      ("text", Some (string_val text));
      ("word_count", Some (number_val word_count));
      ("turn_count", Some (number_val turn_count));
      ("paths", Some paths_val);
      ("weave_source", Some (string_val weave_source));
    ] in
    let fields = if original_timestamp > 0 then
      ("original_timestamp", Some (number_val original_timestamp)) :: base_fields
    else base_fields in
    let attrs = Struct.make ~fields () in

    let command = Protocol.AttestationCommand.make
      ~subjects:[branch]
      ~predicates:["Weave"]
      ~contexts:[context]
      ~actors:["loom"]
      ~attributes:attrs
      ~source:"loom"
      ~source_version:Version.value
      () in

    let request = Protocol.GenerateAttestationRequest.make
      ~auth_token:!auth_token
      ~command
      () in

    let request_bytes = Qntx_plugin.Server.proto_to_string
      (Protocol.GenerateAttestationRequest.to_proto request) in

    let* result = grpc_call
      ~path:"/protocol.ATSStoreService/GenerateAndCreateAttestation"
      ~request_bytes in

    match result with
    | Error msg -> Lwt.return_error msg
    | Ok payload ->
      let reader = Ocaml_protoc_plugin.Reader.create payload in
      (match Protocol.GenerateAttestationResponse.from_proto reader with
       | Ok resp when resp.success ->
         Printf.printf "[ats] Weave created for branch %s\n%!" branch;
         Lwt.return_ok ()
       | Ok resp ->
         Printf.eprintf "[ats] Weave creation failed: %s\n%!" resp.error;
         Lwt.return_error resp.error
       | Error e ->
         let msg = Ocaml_protoc_plugin.Result.show_error e in
         Printf.eprintf "[ats] Proto decode error: %s\n%!" msg;
         Lwt.return_error msg)

(* --- WeaveComplete attestation --- *)

(* Written after a successful JSONL import. Records file size and line count
 * so we can detect stale sessions (file grew since last import). *)
let create_weave_complete ~session_id ~file_path ~file_size ~line_count ~weave_count =
  if !endpoint = "" then (
    Printf.eprintf "[ats] ATS client not configured, dropping WeaveComplete\n%!";
    Lwt.return_error "ATS client not configured"
  ) else
    let open Lwt.Syntax in
    let open Qntx_plugin_proto.Struct.Google.Protobuf in
    let string_val s = Value.make ~kind:(`String_value s) () in
    let number_val n = Value.make ~kind:(`Number_value (Float.of_int n)) () in
    let attrs = Struct.make ~fields:[
      ("file_path", Some (string_val file_path));
      ("file_size", Some (number_val file_size));
      ("line_count", Some (number_val line_count));
      ("weave_count", Some (number_val weave_count));
    ] () in

    let command = Protocol.AttestationCommand.make
      ~subjects:[session_id]
      ~predicates:["WeaveComplete"]
      ~contexts:["import"]
      ~actors:["loom"]
      ~attributes:attrs
      ~source:"loom"
      ~source_version:Version.value
      () in

    let request = Protocol.GenerateAttestationRequest.make
      ~auth_token:!auth_token
      ~command
      () in

    let request_bytes = Qntx_plugin.Server.proto_to_string
      (Protocol.GenerateAttestationRequest.to_proto request) in

    let* result = grpc_call
      ~path:"/protocol.ATSStoreService/GenerateAndCreateAttestation"
      ~request_bytes in

    match result with
    | Error msg -> Lwt.return_error msg
    | Ok payload ->
      let reader = Ocaml_protoc_plugin.Reader.create payload in
      (match Protocol.GenerateAttestationResponse.from_proto reader with
       | Ok resp when resp.success ->
         Printf.printf "[ats] WeaveComplete attestation created for session %s\n%!" session_id;
         Lwt.return_ok ()
       | Ok resp ->
         Printf.eprintf "[ats] WeaveComplete creation failed: %s\n%!" resp.error;
         Lwt.return_error resp.error
       | Error e ->
         let msg = Ocaml_protoc_plugin.Result.show_error e in
         Printf.eprintf "[ats] Proto decode error: %s\n%!" msg;
         Lwt.return_error msg)

(* --- Query WeaveComplete attestations --- *)

let get_weave_completes ?subjects () =
  if !endpoint = "" then (
    Printf.eprintf "[ats] ATS client not configured\n%!";
    Lwt.return_error "ATS client not configured"
  ) else
    let open Lwt.Syntax in
    let filter = Protocol.AttestationFilter.make
      ?subjects
      ~predicates:["WeaveComplete"]
      () in
    let request = Protocol.GetAttestationsRequest.make
      ~auth_token:!auth_token
      ~filter
      () in
    let request_bytes = Qntx_plugin.Server.proto_to_string
      (Protocol.GetAttestationsRequest.to_proto request) in

    let* result = grpc_call
      ~path:"/protocol.ATSStoreService/GetAttestations"
      ~request_bytes in

    match result with
    | Error msg -> Lwt.return_error msg
    | Ok payload ->
      let reader = Ocaml_protoc_plugin.Reader.create payload in
      (match Protocol.GetAttestationsResponse.from_proto reader with
       | Ok resp when resp.success ->
         Lwt.return_ok resp.attestations
       | Ok resp ->
         Lwt.return_error resp.error
       | Error e ->
         let msg = Ocaml_protoc_plugin.Result.show_error e in
         Lwt.return_error (Printf.sprintf "proto decode error: %s" msg))

(* --- Query weaves --- *)

let get_weaves ?subjects ?contexts ?limit () =
  if !endpoint = "" then (
    Printf.eprintf "[ats] ATS client not configured\n%!";
    Lwt.return_error "ATS client not configured"
  ) else
    let open Lwt.Syntax in
    let filter = Protocol.AttestationFilter.make
      ?subjects
      ~predicates:["Weave"]
      ?contexts
      ?limit
      () in
    let request = Protocol.GetAttestationsRequest.make
      ~auth_token:!auth_token
      ~filter
      () in
    let request_bytes = Qntx_plugin.Server.proto_to_string
      (Protocol.GetAttestationsRequest.to_proto request) in

    let* result = grpc_call
      ~path:"/protocol.ATSStoreService/GetAttestations"
      ~request_bytes in

    match result with
    | Error msg -> Lwt.return_error msg
    | Ok payload ->
      let reader = Ocaml_protoc_plugin.Reader.create payload in
      (match Protocol.GetAttestationsResponse.from_proto reader with
       | Ok resp when resp.success ->
         Lwt.return_ok resp.attestations
       | Ok resp ->
         Lwt.return_error resp.error
       | Error e ->
         let msg = Ocaml_protoc_plugin.Result.show_error e in
         Lwt.return_error (Printf.sprintf "proto decode error: %s" msg))
