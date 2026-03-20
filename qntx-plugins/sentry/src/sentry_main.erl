%%% @doc Escript entry point for the sentry plugin.
%%%
%%% Parses CLI flags, starts the OTP application, binds the gRPC server,
%%% and prints the QNTX_PLUGIN_PORT=N announcement for the plugin manager.
-module(sentry_main).
-export([main/1]).

main(Args) ->
    case parse_args(Args, #{port => 9040, version => false, log_level => "info"}) of
        #{version := true} ->
            {ok, Vsn} = application:get_key(sentry, vsn),
            io:format("qntx-sentry-plugin ~s~n", [Vsn]),
            halt(0);
        #{port := Port} ->
            %% Initialize HPACK Huffman trie (stored in persistent_term)
            sentry_hpack:init(),

            %% Start OTP application (supervisor + monitor gen_server)
            {ok, _} = application:ensure_all_started(sentry),

            %% Bind gRPC listener, trying up to 64 consecutive ports
            case sentry_h2:listen(Port, 64) of
                {ok, ActualPort, ListenSock} ->
                    io:format("QNTX_PLUGIN_PORT=~w~n", [ActualPort]),
                    io:format("[sentry] gRPC server listening on 127.0.0.1:~w~n", [ActualPort]),
                    io:format("[sentry] OTP supervisor plugin v~s ready~n",
                              [sentry_plugin:version()]),

                    %% Accept connections forever (blocks main process)
                    sentry_h2:accept_loop(ListenSock);
                {error, Reason} ->
                    io:format("[sentry] could not bind to port ~w: ~p~n", [Port, Reason]),
                    halt(1)
            end
    end.

parse_args([], Opts) ->
    Opts;
parse_args(["--port", PortStr | Rest], Opts) ->
    parse_args(Rest, Opts#{port => list_to_integer(PortStr)});
parse_args(["--address", Addr | Rest], Opts) ->
    %% Extract port from "host:port" format
    case string:rchr(Addr, $:) of
        0 -> parse_args(Rest, Opts);
        Pos ->
            PortStr = string:substr(Addr, Pos + 1),
            parse_args(Rest, Opts#{port => list_to_integer(PortStr)})
    end;
parse_args(["--version" | _], Opts) ->
    Opts#{version => true};
parse_args(["--log-level", _Level | Rest], Opts) ->
    parse_args(Rest, Opts);
parse_args([_ | Rest], Opts) ->
    parse_args(Rest, Opts).
