%%% @doc Plugin protocol handlers — implements DomainPluginService RPCs.
%%%
%%% Each RPC maps to a function clause in handle_rpc/2 via the gRPC path.
%%% The handler gets raw protobuf bytes and returns raw protobuf bytes.
%%% sentry_proto handles the encoding/decoding.
-module(sentry_plugin).
-export([handle_rpc/2, version/0]).

-define(PLUGIN_NAME, <<"sentry">>).
-define(PLUGIN_VERSION, <<"0.1.0">>).

version() -> ?PLUGIN_VERSION.

%% -----------------------------------------------------------------------
%% RPC dispatcher
%% -----------------------------------------------------------------------

handle_rpc(<<"/protocol.DomainPluginService/Metadata">>, _Data) ->
    sentry_proto:encode_metadata_response(#{
        name => ?PLUGIN_NAME,
        version => ?PLUGIN_VERSION,
        qntx_version => <<">= 0.1.0">>,
        description => <<"OTP supervisor plugin — process health via supervision trees">>,
        author => <<"QNTX">>,
        license => <<"MIT">>
    });

handle_rpc(<<"/protocol.DomainPluginService/Initialize">>, Data) ->
    Req = sentry_proto:decode_initialize_request(Data),
    io:format("[sentry] Initialize: endpoints ats=~s queue=~s schedule=~s~n", [
        maps:get(ats_store_endpoint, Req),
        maps:get(queue_endpoint, Req),
        maps:get(schedule_endpoint, Req)
    ]),
    sentry_proto:encode_initialize_response(#{
        handler_names => [],
        schedules => []
    });

handle_rpc(<<"/protocol.DomainPluginService/Shutdown">>, _Data) ->
    io:format("[sentry] Shutdown~n"),
    sentry_proto:encode_empty();

handle_rpc(<<"/protocol.DomainPluginService/Health">>, _Data) ->
    MonitorState = sentry_monitor:get_state(),
    UptimeSecs = maps:get(uptime_secs, MonitorState, 0),
    ProcessCount = maps:get(process_count, MonitorState, 0),
    MemoryMB = maps:get(memory_mb, MonitorState, 0),
    OtpRelease = maps:get(otp_release, MonitorState, "unknown"),
    SupervisedCount = maps:get(supervised_processes, MonitorState, 0),
    HealthChecks = maps:get(health_checks, MonitorState, 0),

    sentry_proto:encode_health_response(#{
        healthy => true,
        message => iolist_to_binary(io_lib:format(
            "OTP supervisor active, ~B processes, uptime ~Bs",
            [SupervisedCount, UptimeSecs])),
        details => #{
            <<"version">> => ?PLUGIN_VERSION,
            <<"otp_release">> => ensure_bin(OtpRelease),
            <<"uptime_secs">> => integer_to_binary(UptimeSecs),
            <<"beam_processes">> => integer_to_binary(ProcessCount),
            <<"memory_mb">> => integer_to_binary(MemoryMB),
            <<"supervised_count">> => integer_to_binary(SupervisedCount),
            <<"health_checks">> => integer_to_binary(HealthChecks)
        }
    });

handle_rpc(<<"/protocol.DomainPluginService/HandleHTTP">>, Data) ->
    Req = sentry_proto:decode_http_request(Data),
    handle_http(maps:get(method, Req), maps:get(path, Req));

handle_rpc(<<"/protocol.DomainPluginService/ConfigSchema">>, _Data) ->
    sentry_proto:encode_config_schema_response(#{fields => #{}});

handle_rpc(<<"/protocol.DomainPluginService/RegisterGlyphs">>, _Data) ->
    sentry_proto:encode_glyph_def_response(#{glyphs => []});

handle_rpc(<<"/protocol.DomainPluginService/ExecuteJob">>, _Data) ->
    sentry_proto:encode_execute_job_response(#{
        success => false,
        error => <<"sentry does not execute jobs — it supervises processes">>,
        plugin_version => ?PLUGIN_VERSION
    });

handle_rpc(Path, _Data) ->
    io:format("[sentry] Unknown RPC: ~s~n", [Path]),
    sentry_proto:encode_empty().

%% -----------------------------------------------------------------------
%% HTTP handlers
%% -----------------------------------------------------------------------

handle_http(<<"GET">>, <<"/status">>) ->
    MonitorState = sentry_monitor:get_state(),
    Json = iolist_to_binary(io_lib:format(
        "{\"name\":\"sentry\",\"version\":\"~s\","
        "\"otp_release\":\"~s\","
        "\"uptime_secs\":~B,"
        "\"beam_processes\":~B,"
        "\"memory_mb\":~B,"
        "\"supervised_count\":~B}",
        [?PLUGIN_VERSION,
         maps:get(otp_release, MonitorState, "unknown"),
         maps:get(uptime_secs, MonitorState, 0),
         maps:get(process_count, MonitorState, 0),
         maps:get(memory_mb, MonitorState, 0),
         maps:get(supervised_processes, MonitorState, 0)])),
    sentry_proto:encode_http_response(#{
        status_code => 200,
        headers => [#{name => <<"Content-Type">>, values => [<<"application/json">>]}],
        body => Json
    });

handle_http(<<"GET">>, <<"/supervision">>) ->
    %% Return supervision tree structure
    Children = supervisor:which_children(sentry_sup),
    ChildInfo = lists:map(fun({Id, Pid, Type, _Mods}) ->
        Status = case is_pid(Pid) of
            true -> <<"running">>;
            false -> <<"stopped">>
        end,
        iolist_to_binary(io_lib:format(
            "{\"id\":\"~s\",\"pid\":\"~p\",\"type\":\"~s\",\"status\":\"~s\"}",
            [Id, Pid, Type, Status]))
    end, Children),
    Json = iolist_to_binary([<<"[">>, lists:join(<<",">>, ChildInfo), <<"]">>]),
    sentry_proto:encode_http_response(#{
        status_code => 200,
        headers => [#{name => <<"Content-Type">>, values => [<<"application/json">>]}],
        body => Json
    });

handle_http(_Method, Path) ->
    sentry_proto:encode_http_response(#{
        status_code => 404,
        headers => [#{name => <<"Content-Type">>, values => [<<"application/json">>]}],
        body => iolist_to_binary([<<"{\"error\":\"not found: ">>, Path, <<"\"}">>])
    }).

%% -----------------------------------------------------------------------
%% Helpers
%% -----------------------------------------------------------------------

ensure_bin(B) when is_binary(B) -> B;
ensure_bin(L) when is_list(L) -> list_to_binary(L);
ensure_bin(A) when is_atom(A) -> atom_to_binary(A, utf8).
