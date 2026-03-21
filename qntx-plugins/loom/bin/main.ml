let () =
  Qntx_plugin.Server.run
    ~name:"loom"
    ~build_service:Qntx_loom.Plugin.build_service
    ~on_start:(fun () ->
      Lwt.async (fun () ->
        Lwt.catch
          (fun () -> Qntx_loom.Udp_listener.start ())
          (fun exn ->
            Printf.eprintf "[loom] UDP listener failed: %s\n%!" (Printexc.to_string exn);
            Lwt.return_unit));
      (* Catch up on OTLPSpan attestations from ATS on startup *)
      Lwt.async (fun () ->
        Lwt.catch
          (fun () ->
            let open Lwt.Syntax in
            let* results = Qntx_loom.Ats_reader.ingest () in
            let* () = Qntx_loom.Http_api.persist_otlp_weaves results in
            Lwt.return_unit)
          (fun exn ->
            Printf.eprintf "[loom] ATS reader failed: %s\n%!" (Printexc.to_string exn);
            Lwt.return_unit));
      Qntx_loom.Http_api.start ())
    ~on_shutdown:Qntx_loom.Plugin.flush_and_persist
    ()
