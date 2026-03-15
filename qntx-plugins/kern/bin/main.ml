(* kern entry point — gRPC plugin server for Ax query parsing.
 * Same pattern as loom: parse --port, start h2c server, announce port. *)

let () =
  let port = ref 0 in
  let spec = [
    ("--port", Arg.Set_int port, "gRPC listen port (assigned by QNTX)");
  ] in
  Arg.parse spec (fun _ -> ()) "kern [--port PORT]";

  if !port = 0 then (
    Printf.eprintf "Error: --port is required\n";
    exit 1
  );

  Printf.printf "[kern] Starting on port %d\n%!" !port;

  Lwt_main.run (
    let open Lwt.Syntax in
    let sigterm_waiter, sigterm_wakener = Lwt.wait () in
    let _sigterm_handler = Lwt_unix.on_signal Sys.sigterm (fun _signum ->
      Lwt.wakeup sigterm_wakener ()
    ) in
    let serve = Kern.Plugin.serve !port in
    let shutdown =
      let* () = sigterm_waiter in
      Printf.printf "[kern] SIGTERM received — shutting down\n%!";
      Lwt.return_unit
    in
    Lwt.pick [serve; shutdown]
  )
