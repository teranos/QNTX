%%% @doc Protobuf wire format codec using Erlang binary pattern matching.
%%%
%%% Messages are represented as maps. Encode/decode functions are
%%% hand-written per message type (no code generation, no reflection).
%%% Erlang's binary syntax makes this clean:
%%%
%%%   decode_varint(<<0:1, Val:7, Rest/binary>>) -> {Val, Rest}
%%%
%%% Compare to the 50+ lines of varint decode in the D version.
-module(sentry_proto).
-export([
    encode_metadata_response/1,
    encode_initialize_response/1,
    encode_health_response/1,
    encode_http_response/1,
    encode_config_schema_response/1,
    encode_glyph_def_response/1,
    encode_execute_job_response/1,
    encode_empty/0,
    decode_initialize_request/1,
    decode_http_request/1,
    decode_execute_job_request/1
]).

%% -----------------------------------------------------------------------
%% Varint
%% -----------------------------------------------------------------------

encode_varint(N) when N < 128 ->
    <<N>>;
encode_varint(N) ->
    <<1:1, (N band 16#7F):7, (encode_varint(N bsr 7))/binary>>.

decode_varint(Bin) ->
    decode_varint(Bin, 0, 0).

decode_varint(<<0:1, Val:7, Rest/binary>>, Acc, Shift) ->
    {Acc bor (Val bsl Shift), Rest};
decode_varint(<<1:1, Val:7, Rest/binary>>, Acc, Shift) ->
    decode_varint(Rest, Acc bor (Val bsl Shift), Shift + 7);
decode_varint(<<>>, Acc, _Shift) ->
    {Acc, <<>>}.

%% -----------------------------------------------------------------------
%% Tags
%% -----------------------------------------------------------------------

encode_tag(FieldNum, WireType) ->
    encode_varint((FieldNum bsl 3) bor WireType).

%% Wire types
-define(VARINT, 0).
-define(FIXED64, 1).
-define(LEN_DELIM, 2).

%% -----------------------------------------------------------------------
%% Field encoders
%% -----------------------------------------------------------------------

encode_string_field(_FieldNum, <<>>) -> <<>>;
encode_string_field(FieldNum, Val) when is_binary(Val) ->
    <<(encode_tag(FieldNum, ?LEN_DELIM))/binary,
      (encode_varint(byte_size(Val)))/binary,
      Val/binary>>;
encode_string_field(FieldNum, Val) when is_list(Val) ->
    encode_string_field(FieldNum, list_to_binary(Val)).

encode_bytes_field(_FieldNum, <<>>) -> <<>>;
encode_bytes_field(FieldNum, Val) ->
    <<(encode_tag(FieldNum, ?LEN_DELIM))/binary,
      (encode_varint(byte_size(Val)))/binary,
      Val/binary>>.

encode_bool_field(_FieldNum, false) -> <<>>;
encode_bool_field(FieldNum, true) ->
    <<(encode_tag(FieldNum, ?VARINT))/binary, 1>>.

encode_int32_field(_FieldNum, 0) -> <<>>;
encode_int32_field(FieldNum, Val) ->
    <<(encode_tag(FieldNum, ?VARINT))/binary,
      (encode_varint(Val))/binary>>.

encode_double_field(_FieldNum, 0.0) -> <<>>;
encode_double_field(FieldNum, Val) ->
    <<(encode_tag(FieldNum, ?FIXED64))/binary,
      Val:64/float-little>>.

encode_map_field(FieldNum, Map) when is_map(Map) ->
    maps:fold(fun(K, V, Acc) ->
        Entry = <<(encode_string_field(1, ensure_bin(K)))/binary,
                  (encode_string_field(2, ensure_bin(V)))/binary>>,
        <<Acc/binary,
          (encode_tag(FieldNum, ?LEN_DELIM))/binary,
          (encode_varint(byte_size(Entry)))/binary,
          Entry/binary>>
    end, <<>>, Map);
encode_map_field(_FieldNum, _) -> <<>>.

encode_repeated_strings(_FieldNum, []) -> <<>>;
encode_repeated_strings(FieldNum, Strings) ->
    lists:foldl(fun(S, Acc) ->
        <<Acc/binary, (encode_string_field(FieldNum, ensure_bin(S)))/binary>>
    end, <<>>, Strings).

encode_embedded(FieldNum, Bin) ->
    <<(encode_tag(FieldNum, ?LEN_DELIM))/binary,
      (encode_varint(byte_size(Bin)))/binary,
      Bin/binary>>.

%% -----------------------------------------------------------------------
%% Message encoders (mirrors domain.proto)
%% -----------------------------------------------------------------------

encode_empty() -> <<>>.

encode_metadata_response(#{name := Name, version := Vsn} = M) ->
    <<(encode_string_field(1, Name))/binary,
      (encode_string_field(2, Vsn))/binary,
      (encode_string_field(3, maps:get(qntx_version, M, <<>>)))/binary,
      (encode_string_field(4, maps:get(description, M, <<>>)))/binary,
      (encode_string_field(5, maps:get(author, M, <<>>)))/binary,
      (encode_string_field(6, maps:get(license, M, <<>>)))/binary>>.

encode_initialize_response(#{} = M) ->
    <<(encode_repeated_strings(1, maps:get(handler_names, M, [])))/binary,
      (encode_repeated_schedules(2, maps:get(schedules, M, [])))/binary>>.

encode_repeated_schedules(_FieldNum, []) -> <<>>;
encode_repeated_schedules(FieldNum, Schedules) ->
    lists:foldl(fun(S, Acc) ->
        Encoded = encode_schedule_info(S),
        <<Acc/binary, (encode_embedded(FieldNum, Encoded))/binary>>
    end, <<>>, Schedules).

encode_schedule_info(#{} = S) ->
    <<(encode_string_field(1, maps:get(handler_name, S, <<>>)))/binary,
      (encode_int32_field(2, maps:get(interval_seconds, S, 0)))/binary,
      (encode_bool_field(3, maps:get(enabled_by_default, S, false)))/binary,
      (encode_string_field(4, maps:get(description, S, <<>>)))/binary,
      (encode_string_field(5, maps:get(ats_code, S, <<>>)))/binary>>.

encode_health_response(#{healthy := Healthy} = M) ->
    <<(encode_bool_field(1, Healthy))/binary,
      (encode_string_field(2, maps:get(message, M, <<>>)))/binary,
      (encode_map_field(3, maps:get(details, M, #{})))/binary>>.

encode_http_response(#{status_code := Status} = M) ->
    <<(encode_int32_field(1, Status))/binary,
      (encode_repeated_headers(2, maps:get(headers, M, [])))/binary,
      (encode_bytes_field(3, maps:get(body, M, <<>>)))/binary>>.

encode_repeated_headers(_FieldNum, []) -> <<>>;
encode_repeated_headers(FieldNum, Headers) ->
    lists:foldl(fun(#{name := N, values := Vs}, Acc) ->
        Encoded = <<(encode_string_field(1, N))/binary,
                    (encode_repeated_strings(2, Vs))/binary>>,
        <<Acc/binary, (encode_embedded(FieldNum, Encoded))/binary>>
    end, <<>>, Headers).

encode_config_schema_response(#{fields := Fields}) ->
    encode_map_field(1, Fields);
encode_config_schema_response(_) -> <<>>.

encode_glyph_def_response(#{glyphs := _Glyphs}) ->
    %% Sentry provides no glyphs (headless plugin)
    <<>>;
encode_glyph_def_response(_) -> <<>>.

encode_execute_job_response(#{success := Success} = M) ->
    <<(encode_bool_field(1, Success))/binary,
      (encode_string_field(2, maps:get(error, M, <<>>)))/binary,
      (encode_bytes_field(3, maps:get(result, M, <<>>)))/binary,
      (encode_int32_field(4, maps:get(progress_current, M, 0)))/binary,
      (encode_int32_field(5, maps:get(progress_total, M, 0)))/binary,
      (encode_double_field(6, maps:get(cost_actual, M, 0.0)))/binary,
      (encode_string_field(8, maps:get(plugin_version, M, <<>>)))/binary>>.

%% -----------------------------------------------------------------------
%% Message decoders
%% -----------------------------------------------------------------------

decode_initialize_request(Bin) ->
    decode_fields(Bin, #{
        ats_store_endpoint => <<>>,
        queue_endpoint => <<>>,
        auth_token => <<>>,
        config => #{},
        schedule_endpoint => <<>>,
        file_service_endpoint => <<>>
    }, fun decode_init_req_field/3).

decode_http_request(Bin) ->
    decode_fields(Bin, #{
        method => <<>>,
        path => <<>>,
        headers => [],
        body => <<>>
    }, fun decode_http_req_field/3).

decode_execute_job_request(Bin) ->
    decode_fields(Bin, #{
        job_id => <<>>,
        handler_name => <<>>,
        payload => <<>>,
        timeout_secs => 0
    }, fun decode_exec_job_field/3).

%% Generic field decoder loop
decode_fields(<<>>, Acc, _FieldFun) ->
    Acc;
decode_fields(Bin, Acc, FieldFun) ->
    {Tag, Rest} = decode_varint(Bin),
    FieldNum = Tag bsr 3,
    WireType = Tag band 7,
    {Value, Rest2} = decode_value(WireType, Rest),
    Acc2 = FieldFun(FieldNum, Value, Acc),
    decode_fields(Rest2, Acc2, FieldFun).

decode_value(?VARINT, Bin) ->
    decode_varint(Bin);
decode_value(?FIXED64, <<Val:64/little, Rest/binary>>) ->
    {{fixed64, Val}, Rest};
decode_value(?LEN_DELIM, Bin) ->
    {Len, Rest} = decode_varint(Bin),
    <<Data:Len/binary, Rest2/binary>> = Rest,
    {Data, Rest2};
decode_value(5, <<Val:32/little, Rest/binary>>) -> %% FIXED32
    {{fixed32, Val}, Rest}.

%% InitializeRequest field decoder
decode_init_req_field(1, Val, Acc) -> Acc#{ats_store_endpoint => Val};
decode_init_req_field(2, Val, Acc) -> Acc#{queue_endpoint => Val};
decode_init_req_field(3, Val, Acc) -> Acc#{auth_token => Val};
decode_init_req_field(4, Val, Acc) ->
    %% Map entry: embedded message with fields 1 (key) and 2 (value)
    {K, V} = decode_map_entry(Val),
    Maps = maps:get(config, Acc),
    Acc#{config => Maps#{K => V}};
decode_init_req_field(5, Val, Acc) -> Acc#{schedule_endpoint => Val};
decode_init_req_field(6, Val, Acc) -> Acc#{file_service_endpoint => Val};
decode_init_req_field(_, _, Acc) -> Acc.

%% HTTPRequest field decoder
decode_http_req_field(1, Val, Acc) -> Acc#{method => Val};
decode_http_req_field(2, Val, Acc) -> Acc#{path => Val};
decode_http_req_field(3, Val, Acc) ->
    Header = decode_http_header(Val),
    Acc#{headers => maps:get(headers, Acc) ++ [Header]};
decode_http_req_field(4, Val, Acc) -> Acc#{body => Val};
decode_http_req_field(_, _, Acc) -> Acc.

%% ExecuteJobRequest field decoder
decode_exec_job_field(1, Val, Acc) -> Acc#{job_id => Val};
decode_exec_job_field(2, Val, Acc) -> Acc#{handler_name => Val};
decode_exec_job_field(3, Val, Acc) -> Acc#{payload => Val};
decode_exec_job_field(4, Val, Acc) -> Acc#{timeout_secs => Val};
decode_exec_job_field(_, _, Acc) -> Acc.

%% Decode a map<string,string> entry
decode_map_entry(Bin) ->
    decode_map_entry(Bin, <<>>, <<>>).

decode_map_entry(<<>>, K, V) -> {K, V};
decode_map_entry(Bin, K, V) ->
    {Tag, Rest} = decode_varint(Bin),
    FieldNum = Tag bsr 3,
    {Val, Rest2} = decode_value(Tag band 7, Rest),
    case FieldNum of
        1 -> decode_map_entry(Rest2, Val, V);
        2 -> decode_map_entry(Rest2, K, Val);
        _ -> decode_map_entry(Rest2, K, V)
    end.

%% Decode an HTTPHeader embedded message
decode_http_header(Bin) ->
    decode_http_header(Bin, <<>>, []).

decode_http_header(<<>>, Name, Values) ->
    #{name => Name, values => lists:reverse(Values)};
decode_http_header(Bin, Name, Values) ->
    {Tag, Rest} = decode_varint(Bin),
    FieldNum = Tag bsr 3,
    {Val, Rest2} = decode_value(Tag band 7, Rest),
    case FieldNum of
        1 -> decode_http_header(Rest2, Val, Values);
        2 -> decode_http_header(Rest2, Name, [Val | Values]);
        _ -> decode_http_header(Rest2, Name, Values)
    end.

%% -----------------------------------------------------------------------
%% Helpers
%% -----------------------------------------------------------------------

ensure_bin(B) when is_binary(B) -> B;
ensure_bin(L) when is_list(L) -> list_to_binary(L);
ensure_bin(A) when is_atom(A) -> atom_to_binary(A, utf8);
ensure_bin(I) when is_integer(I) -> integer_to_binary(I).
