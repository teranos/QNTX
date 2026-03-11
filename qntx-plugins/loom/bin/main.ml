(* qntx-loom entry point
 *
 * Parses --port argument, starts gRPC server implementing DomainPluginService.
 * QNTX launches this binary and connects to it via gRPC.
 *)

let () =
  let port = ref 0 in
  let spec = [
    ("--port", Arg.Set_int port, "gRPC listen port (assigned by QNTX)");
  ] in
  Arg.parse spec (fun _ -> ()) "qntx-loom [--port PORT]";

  if !port = 0 then (
    Printf.eprintf "Error: --port is required\n";
    exit 1
  );

  Printf.printf "[loom] Starting on port %d\n%!" !port;

  Lwt_main.run (
    let open Lwt.Syntax in
    (* Register SIGTERM handler inside the Lwt event loop so async
     * operations (ATS persistence) can run before exit. *)
    let sigterm_waiter, sigterm_wakener = Lwt.wait () in
    let _sigterm_handler = Lwt_unix.on_signal Sys.sigterm (fun _signum ->
      Lwt.wakeup sigterm_wakener ()
    ) in
    (* Race: serve forever OR SIGTERM arrives *)
    let serve = Qntx_loom.Plugin.serve !port in
    let shutdown =
      let* () = sigterm_waiter in
      Printf.printf "[loom] SIGTERM received — flushing buffered weaves\n%!";
      let flushed = Qntx_loom.Stitcher.flush_all () in
      let* () = Lwt_list.iter_p (fun (result : Qntx_loom.Stitcher.stitch_result) ->
        match result.emitted with
        | Some block ->
          let* ats_result = Qntx_loom.Ats_client.create_weave
            ~branch:result.branch
            ~context:result.context
            ~text:block
            ~word_count:(Qntx_loom.Stitcher.word_count block)
            ~turn_count:result.turn_count
          in
          (match ats_result with
           | Ok () ->
             Printf.printf "[loom] Persisted %d-word weave for branch %s\n%!"
               (Qntx_loom.Stitcher.word_count block) result.branch
           | Error msg ->
             Printf.eprintf "[loom] Failed to persist weave for %s: %s\n%!" result.branch msg);
          Lwt.return_unit
        | None -> Lwt.return_unit
      ) flushed in
      Printf.printf "[loom] Flushed %d buffer(s) — exiting\n%!" (List.length flushed);
      Lwt.return_unit
    in
    Lwt.pick [serve; shutdown]
  )
