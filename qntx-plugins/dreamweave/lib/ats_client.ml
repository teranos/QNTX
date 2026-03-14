(* ATSStoreService gRPC client — queries weave attestations
 *
 * Connects to QNTX's ATSStoreService endpoint (received during Initialize)
 * and calls GetAttestations to fetch weaves for the explorer frontend.
 *
 * Mirrors loom's ats_client.ml pattern but reads instead of writes. *)

open Qntx_dreamweave_proto.Atsstore

let proto_to_string writer =
  Ocaml_protoc_plugin.Writer.contents writer

(* --- Connection state --- *)

let endpoint = ref ""
let auth_token = ref ""

let configure ~ats_endpoint ~token =
  endpoint := ats_endpoint;
  auth_token := token;
  Printf.printf "[dreamweave] ATS client configured: %s\n%!" ats_endpoint

(* --- gRPC call helper --- *)

let grpc_call ~path ~request_bytes =
  let host, port =
    match String.split_on_char ':' !endpoint with
    | [h; p] -> (h, int_of_string_opt p |> Option.value ~default:0)
    | _ -> ("127.0.0.1", 0)
  in
  if port = 0 then (
    Printf.eprintf "[dreamweave] Invalid ATS endpoint: %s\n%!" !endpoint;
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
            ~error_handler:(fun _ -> Printf.eprintf "[dreamweave] ATS h2 connection error\n%!")
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
            ~error_handler:(fun _ -> Printf.eprintf "[dreamweave] ATS request error\n%!")
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
        Printf.eprintf "[dreamweave] ATS client error: %s\n%!" (Printexc.to_string exn);
        Lwt.return_error (Printexc.to_string exn))

(* --- Query weaves --- *)

let get_weaves ?subjects ?contexts ?limit () =
  if !endpoint = "" then (
    Printf.eprintf "[dreamweave] ATS client not configured\n%!";
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
    let request_bytes = proto_to_string
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
