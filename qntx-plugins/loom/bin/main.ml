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

  Lwt_main.run (Qntx_loom.Plugin.serve !port)
