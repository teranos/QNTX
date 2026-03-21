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
      Lwt.async (fun () ->
        Lwt.catch
          (fun () -> Qntx_loom.Otlp_receiver.start ())
          (fun exn ->
            Printf.eprintf "[loom] OTLP receiver failed: %s\n%!" (Printexc.to_string exn);
            Lwt.return_unit));
      Qntx_loom.Http_api.start ())
    ~on_shutdown:Qntx_loom.Plugin.flush_and_persist
    ()
