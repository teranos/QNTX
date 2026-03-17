let () =
  Qntx_plugin.Server.run
    ~name:"kern"
    ~build_service:Kern.Plugin.build_service
    ()
