%%% @doc HTTP/2 + gRPC server using raw TCP and binary pattern matching.
%%%
%%% Frame parsing is a single pattern match:
%%%
%%%   <<Len:24, Type:8, Flags:8, _:1, StreamId:31, Payload:Len/binary>>
%%%
%%% Compare to the D version which needs 8 lines of byte-shifting to
%%% extract the same fields. Erlang was built for binary protocols.
-module(sentry_h2).
-export([listen/2, accept_loop/1]).

%% HTTP/2 frame types
-define(DATA, 16#0).
-define(HEADERS, 16#1).
-define(RST_STREAM, 16#3).
-define(SETTINGS, 16#4).
-define(PING, 16#6).
-define(GOAWAY, 16#7).
-define(WINDOW_UPDATE, 16#8).

%% Frame flags
-define(END_STREAM, 16#1).
-define(ACK, 16#1).
-define(END_HEADERS, 16#4).

%% HTTP/2 connection preface
-define(H2_PREFACE, <<"PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n">>).

%% -----------------------------------------------------------------------
%% Listener
%% -----------------------------------------------------------------------

%% @doc Try binding to Port, Port+1, ..., Port+MaxAttempts-1.
%% Returns {ok, ActualPort, ListenSock} or {error, all_ports_in_use}.
listen(Port, MaxAttempts) ->
    listen(Port, Port + MaxAttempts, Port).

listen(Current, Max, _Start) when Current >= Max ->
    {error, all_ports_in_use};
listen(Current, Max, Start) ->
    case gen_tcp:listen(Current, [binary, {packet, raw}, {active, false},
                                   {reuseaddr, true}, {ip, {127,0,0,1}},
                                   {backlog, 5}]) of
        {ok, Sock} ->
            {ok, Current, Sock};
        {error, eaddrinuse} ->
            listen(Current + 1, Max, Start);
        {error, Reason} ->
            {error, Reason}
    end.

%% @doc Accept connections forever. Each connection is handled in a
%% spawned process — if one crashes, the accept loop continues.
%% This is the Erlang way: isolation through processes.
accept_loop(ListenSock) ->
    case gen_tcp:accept(ListenSock) of
        {ok, ClientSock} ->
            spawn(fun() -> serve_connection(ClientSock) end),
            accept_loop(ListenSock);
        {error, closed} ->
            ok;
        {error, _Reason} ->
            accept_loop(ListenSock)
    end.

%% -----------------------------------------------------------------------
%% Connection handler
%% -----------------------------------------------------------------------

serve_connection(Sock) ->
    try
        %% Read and validate HTTP/2 preface
        case recv_exact(Sock, 24) of
            {ok, ?H2_PREFACE} ->
                %% Send server settings
                send_frame(Sock, ?SETTINGS, 0, 0, <<>>),

                %% Read client settings
                case read_frame(Sock) of
                    {ok, {?SETTINGS, _Flags, _StreamId, _Payload}} ->
                        send_frame(Sock, ?SETTINGS, ?ACK, 0, <<>>),

                        %% Grant large window
                        send_window_update(Sock, 0, 1073741823),

                        %% Process frames
                        DynTable = sentry_hpack:new_dyn_table(),
                        frame_loop(Sock, #{}, DynTable);
                    _ ->
                        ok
                end;
            _ ->
                ok
        end
    catch
        _:_ -> ok
    after
        gen_tcp:close(Sock)
    end.

%% Main frame processing loop
frame_loop(Sock, Streams, DynTable) ->
    case read_frame(Sock) of
        {ok, {?SETTINGS, Flags, _StreamId, _Payload}} ->
            case Flags band ?ACK of
                0 -> send_frame(Sock, ?SETTINGS, ?ACK, 0, <<>>);
                _ -> ok
            end,
            frame_loop(Sock, Streams, DynTable);

        {ok, {?PING, _Flags, _StreamId, Payload}} ->
            send_frame(Sock, ?PING, ?ACK, 0, Payload),
            frame_loop(Sock, Streams, DynTable);

        {ok, {?WINDOW_UPDATE, _Flags, _StreamId, _Payload}} ->
            frame_loop(Sock, Streams, DynTable);

        {ok, {?GOAWAY, _Flags, _StreamId, _Payload}} ->
            ok;

        {ok, {?HEADERS, Flags, StreamId, Payload}} ->
            {Headers, DynTable2} = sentry_hpack:decode_headers(Payload, DynTable),
            Path = find_header(<<":path">>, Headers),
            Stream = maps:get(StreamId, Streams, #{method => <<>>, data => <<>>}),
            Stream2 = Stream#{method => Path},
            EndStream = (Flags band ?END_STREAM) =/= 0,
            case EndStream of
                true ->
                    handle_stream(Sock, StreamId, Stream2),
                    Streams2 = maps:remove(StreamId, Streams),
                    frame_loop(Sock, Streams2, DynTable2);
                false ->
                    Streams2 = Streams#{StreamId => Stream2},
                    frame_loop(Sock, Streams2, DynTable2)
            end;

        {ok, {?DATA, Flags, StreamId, Payload}} ->
            Stream = maps:get(StreamId, Streams, #{method => <<>>, data => <<>>}),
            Data = maps:get(data, Stream, <<>>),
            Stream2 = Stream#{data => <<Data/binary, Payload/binary>>},
            %% Send WINDOW_UPDATE for received data
            case byte_size(Payload) > 0 of
                true -> send_window_update(Sock, StreamId, byte_size(Payload));
                false -> ok
            end,
            EndStream = (Flags band ?END_STREAM) =/= 0,
            case EndStream of
                true ->
                    handle_stream(Sock, StreamId, Stream2),
                    Streams2 = maps:remove(StreamId, Streams),
                    frame_loop(Sock, Streams2, DynTable);
                false ->
                    Streams2 = Streams#{StreamId => Stream2},
                    frame_loop(Sock, Streams2, DynTable)
            end;

        {ok, {?RST_STREAM, _Flags, StreamId, _Payload}} ->
            Streams2 = maps:remove(StreamId, Streams),
            frame_loop(Sock, Streams2, DynTable);

        {ok, {_Type, _Flags, _StreamId, _Payload}} ->
            %% Unknown frame type — ignore
            frame_loop(Sock, Streams, DynTable);

        {error, _} ->
            ok
    end.

%% -----------------------------------------------------------------------
%% Stream handler — dispatch gRPC request to plugin handlers
%% -----------------------------------------------------------------------

handle_stream(Sock, StreamId, #{method := Method, data := GrpcData}) ->
    RequestProto = grpc_unframe(GrpcData),
    ResponseProto = sentry_plugin:handle_rpc(Method, RequestProto),

    %% Send response headers
    ResponseHeaders = sentry_hpack:encode_response_headers(),
    send_frame(Sock, ?HEADERS, ?END_HEADERS, StreamId, ResponseHeaders),

    %% Send response data (gRPC-framed)
    GrpcResponse = grpc_frame(ResponseProto),
    send_frame(Sock, ?DATA, 0, StreamId, GrpcResponse),

    %% Send trailers (grpc-status: 0)
    Trailers = sentry_hpack:encode_grpc_trailers(),
    send_frame(Sock, ?HEADERS, ?END_STREAM bor ?END_HEADERS, StreamId, Trailers).

%% -----------------------------------------------------------------------
%% gRPC framing: [compressed:1][length:4][data:N]
%% -----------------------------------------------------------------------

grpc_frame(ProtoBytes) ->
    Len = byte_size(ProtoBytes),
    <<0, Len:32/big, ProtoBytes/binary>>.

grpc_unframe(<<_Compressed, Len:32/big, Data:Len/binary, _/binary>>) ->
    Data;
grpc_unframe(<<>>) ->
    <<>>;
grpc_unframe(_) ->
    <<>>.

%% -----------------------------------------------------------------------
%% Frame I/O
%% -----------------------------------------------------------------------

read_frame(Sock) ->
    case recv_exact(Sock, 9) of
        {ok, <<Len:24, Type:8, Flags:8, _:1, StreamId:31>>} ->
            case Len of
                0 ->
                    {ok, {Type, Flags, StreamId, <<>>}};
                _ ->
                    case recv_exact(Sock, Len) of
                        {ok, Payload} ->
                            {ok, {Type, Flags, StreamId, Payload}};
                        Error ->
                            Error
                    end
            end;
        Error ->
            Error
    end.

send_frame(Sock, Type, Flags, StreamId, Payload) ->
    Len = byte_size(Payload),
    Header = <<Len:24, Type:8, Flags:8, 0:1, StreamId:31>>,
    gen_tcp:send(Sock, <<Header/binary, Payload/binary>>).

send_window_update(Sock, StreamId, Increment) ->
    send_frame(Sock, ?WINDOW_UPDATE, 0, StreamId, <<0:1, Increment:31>>).

%% -----------------------------------------------------------------------
%% Helpers
%% -----------------------------------------------------------------------

recv_exact(Sock, N) ->
    recv_exact(Sock, N, <<>>).

recv_exact(_Sock, 0, Acc) ->
    {ok, Acc};
recv_exact(Sock, Remaining, Acc) ->
    case gen_tcp:recv(Sock, Remaining, 30000) of
        {ok, Data} ->
            Got = byte_size(Data),
            case Got >= Remaining of
                true ->
                    {ok, <<Acc/binary, Data/binary>>};
                false ->
                    recv_exact(Sock, Remaining - Got, <<Acc/binary, Data/binary>>)
            end;
        {error, Reason} ->
            {error, Reason}
    end.

find_header(Name, Headers) ->
    case lists:keyfind(Name, 1, Headers) of
        {Name, Value} -> Value;
        false -> <<>>
    end.
