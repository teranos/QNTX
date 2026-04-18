(* DomainPluginService implementation for qntx-loom
 * Uses qntx-plugin shared library for gRPC boilerplate. *)

open Qntx_plugin_proto.Domain

let name = "loom"
let version = Version.value

(* --- Loom-specific RPC Handlers --- *)

let handle_initialize raw =
  let reader = Ocaml_protoc_plugin.Reader.create raw in
  (match Protocol.InitializeRequest.from_proto reader with
   | Ok req ->
     Printf.printf "[loom] Initialized with ATS endpoint: %s\n%!" req.ats_store_endpoint;
     Ats_client.configure
       ~ats_endpoint:req.ats_store_endpoint
       ~token:req.auth_token
   | Error _ ->
     Printf.printf "[loom] Warning: could not decode InitializeRequest\n%!");
  let resp = Protocol.InitializeResponse.make
    ~handler_names:["stitch"]
    () in
  let encoded = Qntx_plugin.Server.proto_to_string (Protocol.InitializeResponse.to_proto resp) in
  Lwt.return (Grpc.Status.(v OK), Some encoded)

let handle_execute_job raw =
  let open Lwt.Syntax in
  let reader = Ocaml_protoc_plugin.Reader.create raw in
  match Protocol.ExecuteJobRequest.from_proto reader with
  | Ok req ->
    Printf.printf "[loom] ExecuteJob handler=%s job_id=%s (%d bytes payload)\n%!"
      req.handler_name req.job_id (Bytes.length req.payload);
    let payload_str = Bytes.to_string req.payload in
    (match req.handler_name with
     | "stitch" ->
       let results = Stitcher.stitch payload_str in
       List.iter (fun (result : Stitcher.stitch_result) ->
         match result.emitted with
         | Some block ->
           Lwt.async (fun () ->
             let* ats_result = Ats_client.create_weave
               ~branch:result.branch
               ~context:result.context
               ~text:block
               ~word_count:(Stitcher.word_count block)
               ~turn_count:result.turn_count
               ~paths:result.paths
               ~weave_source:"ground"
               ()
             in
             (match ats_result with
              | Ok () -> ()
              | Error msg ->
                Printf.eprintf "[loom] Failed to persist weave: %s\n%!" msg);
             Lwt.return_unit)
         | None -> ()
       ) results;
       let result = List.nth results (List.length results - 1) in
       let result_json = Stitcher.result_to_json result in
       let resp = Protocol.ExecuteJobResponse.make
         ~success:true
         ~result:(Bytes.of_string result_json)
         ~plugin_version:version
         () in
       let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
       Lwt.return (Grpc.Status.(v OK), Some encoded)
     | other ->
       let resp = Protocol.ExecuteJobResponse.make
         ~success:false
         ~error:(Printf.sprintf "unknown handler: %s" other)
         ~plugin_version:version
         () in
       let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
       Lwt.return (Grpc.Status.(v OK), Some encoded))
  | Error e ->
    let msg = Ocaml_protoc_plugin.Result.show_error e in
    let resp = Protocol.ExecuteJobResponse.make
      ~success:false
      ~error:(Printf.sprintf "failed to decode ExecuteJobRequest: %s" msg)
      ~plugin_version:version
      () in
    let encoded = Qntx_plugin.Server.proto_to_string (Protocol.ExecuteJobResponse.to_proto resp) in
    Lwt.return (Grpc.Status.(v OK), Some encoded)

let flush_and_persist () =
  let open Lwt.Syntax in
  let flushed = Stitcher.flush_all () in
  let* () = Lwt_list.iter_p (fun (result : Stitcher.stitch_result) ->
    match result.emitted with
    | Some block ->
      let* ats_result = Ats_client.create_weave
        ~branch:result.branch
        ~context:result.context
        ~text:block
        ~word_count:(Stitcher.word_count block)
        ~turn_count:result.turn_count
        ~paths:result.paths
        ~weave_source:"ground"
        ()
      in
      (match ats_result with
       | Ok () -> ()
       | Error msg ->
         Printf.eprintf "[loom] Failed to persist flushed weave for %s: %s\n%!" result.branch msg);
      Lwt.return_unit
    | None -> Lwt.return_unit
  ) flushed in
  Printf.printf "[loom] Flushed %d weave(s)\n%!" (List.length flushed);
  Lwt.return_unit

(* --- Service --- *)

let build_service () =
  Grpc_lwt.Server.Service.(
    v ()
    |> add_rpc ~name:"Metadata"
         ~rpc:(Unary (Qntx_plugin.Server.handle_metadata ~name ~version
                        ~description:"Commit-scoped transcript stitcher"))
    |> add_rpc ~name:"Initialize"
         ~rpc:(Unary handle_initialize)
    |> add_rpc ~name:"Health"
         ~rpc:(Unary Qntx_plugin.Server.handle_health)
    |> add_rpc ~name:"ExecuteJob"
         ~rpc:(Unary handle_execute_job)
    |> add_rpc ~name:"ConfigSchema"
         ~rpc:(Unary Qntx_plugin.Server.handle_config_schema)
    |> add_rpc ~name:"Shutdown"
         ~rpc:(Unary (Qntx_plugin.Server.handle_shutdown
                        ~on_shutdown:flush_and_persist ~name ()))
  )
