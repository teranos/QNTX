(* UDP listener — receives attestation payloads from Graunde
 *
 * Graunde fires a UDP datagram on every hook event (UserPromptSubmit, Stop, etc.)
 * to avoid adding latency to the Claude Code hook chain. Fire-and-forget from
 * Graunde's side — if loom isn't running, the packet drops silently.
 *
 * This is the primary data ingestion path for loom. The gRPC ExecuteJob path
 * via QNTX's watcher system is the "proper" alternative, but requires Graunde
 * to talk to QNTX over the network — which it currently doesn't do. *)

(* TODO(#676): Handle port conflicts gracefully — retry or log clear error *)
let udp_port = 19470

let start () =
  let open Lwt.Syntax in
  let socket = Lwt_unix.socket Unix.PF_INET Unix.SOCK_DGRAM 0 in
  Unix.setsockopt (Lwt_unix.unix_file_descr socket) Unix.SO_REUSEADDR true;
  let addr = Unix.(ADDR_INET (inet_addr_loopback, udp_port)) in
  let* () = Lwt_unix.bind socket addr in
  Printf.printf "[loom] UDP listener on port %d\n%!" udp_port;

  let buf = Bytes.create 65536 in
  let rec loop () =
    let* (len, _sender) = Lwt_unix.recvfrom socket buf 0 65536 [] in
    let payload = Bytes.sub_string buf 0 len in

    let result = Stitcher.stitch payload in

    (* If the stitcher emitted a weave, persist it via ATSStoreService *)
    (match result.emitted with
     | Some block ->
       Lwt.async (fun () ->
         let* ats_result = Ats_client.create_weave
           ~branch:result.branch
           ~context:result.context
           ~text:block
           ~word_count:(Stitcher.word_count block)
           ~turn_count:result.turn_count
         in
         (match ats_result with
          | Ok () -> ()
          | Error msg ->
            Printf.eprintf "[loom] Failed to persist weave: %s\n%!" msg);
         Lwt.return_unit)
     | None -> ());

    loop ()
  in
  loop ()
