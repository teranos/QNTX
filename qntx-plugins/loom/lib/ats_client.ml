(* ATSStoreService gRPC client — creates weave attestations
 *
 * Connects to QNTX's ATSStoreService endpoint (received during Initialize)
 * and calls GenerateAndCreateAttestation to persist woven blocks.
 *
 * This makes loom a gRPC client in addition to being a server — it receives
 * attestation payloads via ExecuteJob (server role) and emits weaves back
 * via ATSStoreService (client role). *)

open Qntx_loom_proto.Atsstore

let proto_to_string writer =
  Ocaml_protoc_plugin.Writer.contents writer

(* --- Connection state --- *)

(* Set during initialize — the endpoint and auth token for ATSStoreService *)
let endpoint = ref ""
let auth_token = ref ""

let configure ~ats_endpoint ~token =
  endpoint := ats_endpoint;
  auth_token := token;
  Printf.printf "[loom] ATS client configured: %s\n%!" ats_endpoint

(* --- Create a weave attestation --- *)

let create_weave ~branch ~text ~word_count ~turn_count =
  if !endpoint = "" then (
    Printf.eprintf "[loom] ATS client not configured, dropping weave\n%!";
    Lwt.return_error "ATS client not configured"
  ) else
    (* Build the attributes as a google.protobuf.Struct.
     * Struct is a map of string → Value, where Value can be
     * string, number, bool, list, or nested struct.
     * ocaml-protoc-plugin represents oneof fields as polymorphic variants. *)
    let open Google_types.Struct.Google.Protobuf in
    let string_val s =
      Value.make ~kind:(`String_value s) ()
    in
    let number_val n =
      Value.make ~kind:(`Number_value (Float.of_int n)) ()
    in
    let attrs = Struct.make ~fields:[
      ("text", Some (string_val text));
      ("word_count", Some (number_val word_count));
      ("turn_count", Some (number_val turn_count));
    ] () in

    let command = Protocol.AttestationCommand.make
      ~subjects:[branch]
      ~predicates:["Weave"]
      ~contexts:[]
      ~actors:["loom"]
      ~attributes:attrs
      ~source:"loom"
      ~source_version:Version.value
      () in

    let request = Protocol.GenerateAttestationRequest.make
      ~auth_token:!auth_token
      ~command
      () in

    (* Serialize the request to protobuf bytes *)
    let request_bytes = proto_to_string
      (Protocol.GenerateAttestationRequest.to_proto request) in

    (* Parse the endpoint to extract host and port *)
    let host, port =
      match String.split_on_char ':' !endpoint with
      | [h; p] -> (h, int_of_string_opt p |> Option.value ~default:0)
      | _ -> ("127.0.0.1", 0)
    in
    if port = 0 then (
      Printf.eprintf "[loom] Invalid ATS endpoint: %s\n%!" !endpoint;
      Lwt.return_error "invalid ATS endpoint"
    ) else
      let open Lwt.Syntax in
      Lwt.catch
        (fun () ->
          (* Open TCP connection to ATSStoreService *)
          let addr = Unix.(ADDR_INET (inet_addr_of_string host, port)) in
          let socket = Lwt_unix.socket Unix.PF_INET Unix.SOCK_STREAM 0 in
          let* () = Lwt_unix.connect socket addr in

          (* Create h2c client connection.
           * h2c = HTTP/2 cleartext (no TLS) — same as what Go's gRPC uses
           * for localhost communication between QNTX components. *)
          let* conn =
            H2_lwt_unix.Client.create_connection
              ~error_handler:(fun _ -> Printf.eprintf "[loom] ATS h2 connection error\n%!")
              socket
          in

          (* Build the HTTP/2 request for the gRPC method.
           * gRPC maps to HTTP/2: POST + path = /package.Service/Method *)
          let h2_request =
            H2.Request.create
              ~scheme:"http"
              ~headers:(H2.Headers.of_list [
                ("content-type", "application/grpc+proto");
                ("te", "trailers");
              ])
              `POST
              "/protocol.ATSStoreService/GenerateAndCreateAttestation"
          in

          (* Collect the response body into a buffer.
           * H2's reader API is callback-based: schedule_read calls on_read
           * for each chunk and on_eof when done. We use Lwt.wait/wakeup
           * to bridge from callbacks to Lwt promises. *)
          let response_buf = Buffer.create 256 in
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

          (* Send the gRPC-framed request.
           * Grpc.Message.make wraps the protobuf bytes in gRPC's 5-byte
           * length-prefixed framing (1 byte compression flag + 4 byte length). *)
          let body_writer =
            H2_lwt_unix.Client.request conn
              ~flush_headers_immediately:true
              h2_request
              ~error_handler:(fun _ -> Printf.eprintf "[loom] ATS request error\n%!")
              ~response_handler
          in
          let grpc_frame = Grpc.Message.make request_bytes in
          H2.Body.Writer.write_string body_writer grpc_frame;
          H2.Body.Writer.close body_writer;

          (* Wait for the full response *)
          let* () = response_received in

          (* Decode the gRPC-framed response.
           * Copy raw response bytes into a Grpc.Buffer, then extract
           * strips the 5-byte frame header and returns the protobuf payload. *)
          let response_data = Buffer.contents response_buf in
          let grpc_buf = Grpc.Buffer.v () in
          let response_bigstring =
            Bigstringaf.of_string ~off:0 ~len:(String.length response_data) response_data
          in
          Grpc.Buffer.copy_from_bigstringaf
            ~src_off:0 ~src:response_bigstring
            ~dst:grpc_buf ~length:(String.length response_data);

          let decoded =
            match Grpc.Message.extract grpc_buf with
            | Some msg ->
              let reader = Ocaml_protoc_plugin.Reader.create msg in
              (match Protocol.GenerateAttestationResponse.from_proto reader with
               | Ok resp -> Some resp
               | Error e ->
                 Printf.eprintf "[loom] Proto decode error: %s\n%!"
                   (Ocaml_protoc_plugin.Result.show_error e);
                 None)
            | None ->
              Printf.eprintf "[loom] No gRPC message in response (%d bytes)\n%!"
                (String.length response_data);
              None
          in

          let* () = H2_lwt_unix.Client.shutdown conn in

          match decoded with
          | Some resp when resp.success ->
            Printf.printf "[loom] Weave created for branch %s\n%!" branch;
            Lwt.return_ok ()
          | Some resp ->
            Printf.eprintf "[loom] Weave creation failed: %s\n%!" resp.error;
            Lwt.return_error resp.error
          | None ->
            Printf.eprintf "[loom] Could not decode ATS response\n%!";
            Lwt.return_error "decode error")
        (fun exn ->
          Printf.eprintf "[loom] ATS client error: %s\n%!" (Printexc.to_string exn);
          Lwt.return_error (Printexc.to_string exn))
