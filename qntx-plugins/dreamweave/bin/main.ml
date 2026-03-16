let () =
  Qntx_plugin.Server.run
    ~name:"dreamweave"
    ~build_service:Qntx_dreamweave.Plugin.build_service
    ~on_start:Qntx_dreamweave.Plugin.start_http_server
    ()
