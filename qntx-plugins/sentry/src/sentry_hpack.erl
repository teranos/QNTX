%%% @doc HPACK header compression codec (RFC 7541).
%%%
%%% Huffman decode trie is built once at startup and stored in persistent_term.
%%% Erlang's bitstring pattern matching makes Huffman decode natural:
%%%
%%%   decode_bits(<<Bit:1, Rest/bitstring>>, Node) ->
%%%       case maps:get(Bit, Node) of
%%%           {sym, Char} -> [Char | decode_bits(Rest, Root)];
%%%           Next -> decode_bits(Rest, Next)
%%%       end.
%%%
%%% No explicit bit shifting, no manual index tracking.
-module(sentry_hpack).
-export([init/0, decode_headers/2, encode_response_headers/0, encode_grpc_trailers/0]).

%% Persistent term key for Huffman trie
-define(HUFF_TRIE_KEY, sentry_hpack_huffman_trie).

%% -----------------------------------------------------------------------
%% Initialization (call once at startup)
%% -----------------------------------------------------------------------

init() ->
    Trie = build_huffman_trie(),
    persistent_term:put(?HUFF_TRIE_KEY, Trie),
    ok.

%% -----------------------------------------------------------------------
%% HPACK static table (RFC 7541 Appendix A)
%% -----------------------------------------------------------------------

static_entry(1)  -> {<<":authority">>, <<>>};
static_entry(2)  -> {<<":method">>, <<"GET">>};
static_entry(3)  -> {<<":method">>, <<"POST">>};
static_entry(4)  -> {<<":path">>, <<"/">>};
static_entry(5)  -> {<<":path">>, <<"/index.html">>};
static_entry(6)  -> {<<":scheme">>, <<"http">>};
static_entry(7)  -> {<<":scheme">>, <<"https">>};
static_entry(8)  -> {<<":status">>, <<"200">>};
static_entry(9)  -> {<<":status">>, <<"204">>};
static_entry(10) -> {<<":status">>, <<"206">>};
static_entry(11) -> {<<":status">>, <<"304">>};
static_entry(12) -> {<<":status">>, <<"400">>};
static_entry(13) -> {<<":status">>, <<"404">>};
static_entry(14) -> {<<":status">>, <<"500">>};
static_entry(15) -> {<<"accept-charset">>, <<>>};
static_entry(16) -> {<<"accept-encoding">>, <<"gzip, deflate">>};
static_entry(17) -> {<<"accept-language">>, <<>>};
static_entry(18) -> {<<"accept-ranges">>, <<>>};
static_entry(19) -> {<<"accept">>, <<>>};
static_entry(20) -> {<<"access-control-allow-origin">>, <<>>};
static_entry(21) -> {<<"age">>, <<>>};
static_entry(22) -> {<<"allow">>, <<>>};
static_entry(23) -> {<<"authorization">>, <<>>};
static_entry(24) -> {<<"cache-control">>, <<>>};
static_entry(25) -> {<<"content-disposition">>, <<>>};
static_entry(26) -> {<<"content-encoding">>, <<>>};
static_entry(27) -> {<<"content-language">>, <<>>};
static_entry(28) -> {<<"content-length">>, <<>>};
static_entry(29) -> {<<"content-location">>, <<>>};
static_entry(30) -> {<<"content-range">>, <<>>};
static_entry(31) -> {<<"content-type">>, <<>>};
static_entry(32) -> {<<"cookie">>, <<>>};
static_entry(33) -> {<<"date">>, <<>>};
static_entry(34) -> {<<"etag">>, <<>>};
static_entry(35) -> {<<"expect">>, <<>>};
static_entry(36) -> {<<"expires">>, <<>>};
static_entry(37) -> {<<"from">>, <<>>};
static_entry(38) -> {<<"host">>, <<>>};
static_entry(39) -> {<<"if-match">>, <<>>};
static_entry(40) -> {<<"if-modified-since">>, <<>>};
static_entry(41) -> {<<"if-none-match">>, <<>>};
static_entry(42) -> {<<"if-range">>, <<>>};
static_entry(43) -> {<<"if-unmodified-since">>, <<>>};
static_entry(44) -> {<<"last-modified">>, <<>>};
static_entry(45) -> {<<"link">>, <<>>};
static_entry(46) -> {<<"location">>, <<>>};
static_entry(47) -> {<<"max-forwards">>, <<>>};
static_entry(48) -> {<<"proxy-authenticate">>, <<>>};
static_entry(49) -> {<<"proxy-authorization">>, <<>>};
static_entry(50) -> {<<"range">>, <<>>};
static_entry(51) -> {<<"referer">>, <<>>};
static_entry(52) -> {<<"refresh">>, <<>>};
static_entry(53) -> {<<"retry-after">>, <<>>};
static_entry(54) -> {<<"server">>, <<>>};
static_entry(55) -> {<<"set-cookie">>, <<>>};
static_entry(56) -> {<<"strict-transport-security">>, <<>>};
static_entry(57) -> {<<"transfer-encoding">>, <<>>};
static_entry(58) -> {<<"user-agent">>, <<>>};
static_entry(59) -> {<<"vary">>, <<>>};
static_entry(60) -> {<<"via">>, <<>>};
static_entry(61) -> {<<"www-authenticate">>, <<>>};
static_entry(_)  -> {<<>>, <<>>}.

%% -----------------------------------------------------------------------
%% Dynamic table
%% -----------------------------------------------------------------------

-record(dyn_table, {
    entries = [] :: [{binary(), binary()}],
    max_size = 4096 :: non_neg_integer(),
    current_size = 0 :: non_neg_integer()
}).

new_dyn_table() ->
    #dyn_table{}.

dyn_add(#dyn_table{entries = Es, max_size = Max, current_size = Cur} = T, Name, Value) ->
    EntrySize = byte_size(Name) + byte_size(Value) + 32,
    {Es2, Cur2} = dyn_evict(Es, Cur, Max - EntrySize),
    case EntrySize =< Max of
        true ->
            T#dyn_table{entries = [{Name, Value} | Es2],
                        current_size = Cur2 + EntrySize};
        false ->
            T#dyn_table{entries = [], current_size = 0}
    end.

dyn_evict([], Cur, _Space) -> {[], min(Cur, 0)};
dyn_evict(Es, Cur, Space) when Cur =< Space -> {Es, Cur};
dyn_evict(Es, Cur, Space) ->
    Last = lists:last(Es),
    {N, V} = Last,
    Size = byte_size(N) + byte_size(V) + 32,
    dyn_evict(lists:droplast(Es), Cur - Size, Space).

dyn_get(#dyn_table{entries = Es}, Idx) ->
    DynIdx = Idx - 62,
    case DynIdx >= 0 andalso DynIdx < length(Es) of
        true -> lists:nth(DynIdx + 1, Es);
        false -> {<<>>, <<>>}
    end.

lookup_index(Idx, _Dyn) when Idx >= 1, Idx =< 61 ->
    static_entry(Idx);
lookup_index(Idx, Dyn) ->
    dyn_get(Dyn, Idx).

%% -----------------------------------------------------------------------
%% HPACK integer codec
%% -----------------------------------------------------------------------

decode_int(<<>>, _PrefixBits) -> {0, <<>>};
decode_int(Bin, PrefixBits) ->
    <<_Flags:(8 - PrefixBits), Val:PrefixBits, Rest/binary>> = Bin,
    Mask = (1 bsl PrefixBits) - 1,
    case Val < Mask of
        true -> {Val, Rest};
        false -> decode_int_cont(Rest, Val, 0)
    end.

decode_int_cont(<<0:1, B:7, Rest/binary>>, Acc, Shift) ->
    {Acc + (B bsl Shift), Rest};
decode_int_cont(<<1:1, B:7, Rest/binary>>, Acc, Shift) ->
    decode_int_cont(Rest, Acc + (B bsl Shift), Shift + 7).

encode_int(Val, PrefixBits, FlagBits) ->
    Mask = (1 bsl PrefixBits) - 1,
    case Val < Mask of
        true ->
            <<(FlagBits bor Val)>>;
        false ->
            Rem = Val - Mask,
            Rest = encode_int_cont(Rem),
            <<(FlagBits bor Mask), Rest/binary>>
    end.

encode_int_cont(Val) when Val < 128 ->
    <<Val>>;
encode_int_cont(Val) ->
    <<1:1, (Val band 16#7F):7, (encode_int_cont(Val bsr 7))/binary>>.

%% -----------------------------------------------------------------------
%% HPACK string codec
%% -----------------------------------------------------------------------

decode_string(<<>>) -> {<<>>, <<>>};
decode_string(<<IsHuffman:1, _:7, _/binary>> = Bin) ->
    {Len, Rest} = decode_int(Bin, 7),
    <<Data:Len/binary, Rest2/binary>> = Rest,
    case IsHuffman of
        1 -> {huffman_decode(Data), Rest2};
        0 -> {Data, Rest2}
    end.

encode_string(Val) ->
    Len = byte_size(Val),
    <<(encode_int(Len, 7, 0))/binary, Val/binary>>.

%% -----------------------------------------------------------------------
%% HPACK header block decoder
%% -----------------------------------------------------------------------

%% @doc Decode an HPACK header block. Returns {Headers, UpdatedDynTable}.
%% Headers is a list of {Name, Value} tuples.
decode_headers(Bin, DynTable) ->
    decode_headers(Bin, DynTable, []).

decode_headers(<<>>, Dyn, Acc) ->
    {lists:reverse(Acc), Dyn};
decode_headers(<<1:1, _:7, _/binary>> = Bin, Dyn, Acc) ->
    %% Indexed header field (bit pattern 1xxxxxxx)
    {Idx, Rest} = decode_int(Bin, 7),
    Header = lookup_index(Idx, Dyn),
    decode_headers(Rest, Dyn, [Header | Acc]);
decode_headers(<<01:2, _:6, _/binary>> = Bin, Dyn, Acc) ->
    %% Literal with incremental indexing (bit pattern 01xxxxxx)
    {Idx, Rest} = decode_int(Bin, 6),
    {Name, Rest2} = case Idx of
        0 -> decode_string(Rest);
        _ -> {N, _} = lookup_index(Idx, Dyn), {N, Rest}
    end,
    {Value, Rest3} = decode_string(Rest2),
    Dyn2 = dyn_add(Dyn, Name, Value),
    decode_headers(Rest3, Dyn2, [{Name, Value} | Acc]);
decode_headers(<<0000:4, _:4, _/binary>> = Bin, Dyn, Acc) ->
    %% Literal without indexing (bit pattern 0000xxxx)
    {Idx, Rest} = decode_int(Bin, 4),
    {Name, Rest2} = case Idx of
        0 -> decode_string(Rest);
        _ -> {N, _} = lookup_index(Idx, Dyn), {N, Rest}
    end,
    {Value, Rest3} = decode_string(Rest2),
    decode_headers(Rest3, Dyn, [{Name, Value} | Acc]);
decode_headers(<<0001:4, _:4, _/binary>> = Bin, Dyn, Acc) ->
    %% Literal never indexed (bit pattern 0001xxxx)
    {Idx, Rest} = decode_int(Bin, 4),
    {Name, Rest2} = case Idx of
        0 -> decode_string(Rest);
        _ -> {N, _} = lookup_index(Idx, Dyn), {N, Rest}
    end,
    {Value, Rest3} = decode_string(Rest2),
    decode_headers(Rest3, Dyn, [{Name, Value} | Acc]);
decode_headers(<<001:3, _:5, _/binary>> = Bin, Dyn, Acc) ->
    %% Dynamic table size update (bit pattern 001xxxxx)
    {NewSize, Rest} = decode_int(Bin, 5),
    Dyn2 = Dyn#dyn_table{max_size = NewSize},
    decode_headers(Rest, Dyn2, Acc);
decode_headers(<<_:8, Rest/binary>>, Dyn, Acc) ->
    %% Unknown — skip byte
    decode_headers(Rest, Dyn, Acc).

%% -----------------------------------------------------------------------
%% HPACK header encoder (minimal — no Huffman, literals only)
%% -----------------------------------------------------------------------

encode_literal_header(Name, Value) ->
    <<0, (encode_string(Name))/binary, (encode_string(Value))/binary>>.

encode_indexed_name_header(NameIdx, Value) ->
    <<(encode_int(NameIdx, 4, 0))/binary, (encode_string(Value))/binary>>.

encode_indexed_header(Idx) ->
    encode_int(Idx, 7, 16#80).

%% Encode response headers: :status 200 + content-type: application/grpc
encode_response_headers() ->
    <<(encode_indexed_header(8))/binary,
      (encode_indexed_name_header(31, <<"application/grpc">>))/binary>>.

%% Encode gRPC trailers: grpc-status: 0
encode_grpc_trailers() ->
    encode_literal_header(<<"grpc-status">>, <<"0">>).

%% -----------------------------------------------------------------------
%% Huffman decode (RFC 7541 Appendix B)
%% -----------------------------------------------------------------------

huffman_decode(Data) ->
    Trie = persistent_term:get(?HUFF_TRIE_KEY),
    Bits = binary_to_bitstring(Data),
    huffman_decode_bits(Bits, Trie, Trie, []).

binary_to_bitstring(Bin) -> Bin.

huffman_decode_bits(<<>>, _Root, _Node, Acc) ->
    list_to_binary(lists:reverse(Acc));
huffman_decode_bits(<<Bit:1, Rest/bitstring>>, Root, Node, Acc) ->
    case maps:get(Bit, Node, undefined) of
        undefined ->
            %% Padding bits at end — done
            list_to_binary(lists:reverse(Acc));
        {sym, 256} ->
            %% EOS symbol — done
            list_to_binary(lists:reverse(Acc));
        {sym, Char} ->
            huffman_decode_bits(Rest, Root, Root, [Char | Acc]);
        NextNode ->
            huffman_decode_bits(Rest, Root, NextNode, Acc)
    end.

%% -----------------------------------------------------------------------
%% Huffman trie builder (RFC 7541 Appendix B table)
%% -----------------------------------------------------------------------

build_huffman_trie() ->
    Table = huffman_table(),
    lists:foldl(fun({Sym, Code, Bits}, Trie) ->
        trie_insert(Trie, Code, Bits, Sym)
    end, #{}, Table).

trie_insert(_Node, _Code, 0, Sym) ->
    {sym, Sym};
trie_insert(Node, Code, BitsLeft, Sym) when is_map(Node) ->
    Bit = (Code bsr (BitsLeft - 1)) band 1,
    Child = maps:get(Bit, Node, #{}),
    Node#{Bit => trie_insert(Child, Code, BitsLeft - 1, Sym)}.

%% RFC 7541 Appendix B — full Huffman code table
%% {Symbol, Code, BitLength}
huffman_table() ->
    [{0,16#1ff8,13},{1,16#7fffd8,23},{2,16#fffffe2,28},{3,16#fffffe3,28},
     {4,16#fffffe4,28},{5,16#fffffe5,28},{6,16#fffffe6,28},{7,16#fffffe7,28},
     {8,16#fffffe8,28},{9,16#ffffea,24},{10,16#3ffffffc,30},{11,16#fffffe9,28},
     {12,16#fffffea,28},{13,16#3ffffffd,30},{14,16#fffffeb,28},{15,16#fffffec,28},
     {16,16#fffffed,28},{17,16#fffffee,28},{18,16#fffffef,28},{19,16#ffffff0,28},
     {20,16#ffffff1,28},{21,16#ffffff2,28},{22,16#3ffffffe,30},{23,16#ffffff3,28},
     {24,16#ffffff4,28},{25,16#ffffff5,28},{26,16#ffffff6,28},{27,16#ffffff7,28},
     {28,16#ffffff8,28},{29,16#ffffff9,28},{30,16#ffffffa,28},{31,16#ffffffb,28},
     {32,16#14,6},{33,16#3f8,10},{34,16#3f9,10},{35,16#ffa,12},
     {36,16#1ff9,13},{37,16#15,6},{38,16#f8,8},{39,16#7fa,11},
     {40,16#3fa,10},{41,16#3fb,10},{42,16#f9,8},{43,16#7fb,11},
     {44,16#fa,8},{45,16#16,6},{46,16#17,6},{47,16#18,6},
     {48,16#0,5},{49,16#1,5},{50,16#2,5},{51,16#19,6},
     {52,16#1a,6},{53,16#1b,6},{54,16#1c,6},{55,16#1d,6},
     {56,16#1e,6},{57,16#1f,6},{58,16#5c,7},{59,16#fb,8},
     {60,16#7ffc,15},{61,16#20,6},{62,16#ffb,12},{63,16#3fc,10},
     {64,16#1ffa,13},{65,16#21,6},{66,16#5d,7},{67,16#5e,7},
     {68,16#5f,7},{69,16#60,7},{70,16#61,7},{71,16#62,7},
     {72,16#63,7},{73,16#64,7},{74,16#65,7},{75,16#66,7},
     {76,16#67,7},{77,16#68,7},{78,16#69,7},{79,16#6a,7},
     {80,16#6b,7},{81,16#6c,7},{82,16#6d,7},{83,16#6e,7},
     {84,16#6f,7},{85,16#70,7},{86,16#71,7},{87,16#72,7},
     {88,16#fc,8},{89,16#73,7},{90,16#fd,8},{91,16#1ffb,13},
     {92,16#7fff0,19},{93,16#1ffc,13},{94,16#3ffc,14},{95,16#22,6},
     {96,16#7ffd,15},{97,16#3,5},{98,16#23,6},{99,16#4,5},
     {100,16#24,6},{101,16#5,5},{102,16#25,6},{103,16#26,6},
     {104,16#27,6},{105,16#6,5},{106,16#74,7},{107,16#75,7},
     {108,16#28,6},{109,16#29,6},{110,16#2a,6},{111,16#7,5},
     {112,16#2b,6},{113,16#76,7},{114,16#2c,6},{115,16#8,5},
     {116,16#9,5},{117,16#2d,6},{118,16#77,7},{119,16#78,7},
     {120,16#79,7},{121,16#7a,7},{122,16#7b,7},{123,16#7fffe,19},
     {124,16#7fc,11},{125,16#3ffd,14},{126,16#1ffd,13},{127,16#ffffffc,28},
     {128,16#fffe6,20},{129,16#3fffd2,22},{130,16#fffe7,20},{131,16#fffe8,20},
     {132,16#3fffd3,22},{133,16#3fffd4,22},{134,16#3fffd5,22},{135,16#7fffd9,23},
     {136,16#3fffd6,22},{137,16#7fffda,23},{138,16#7fffdb,23},{139,16#7fffdc,23},
     {140,16#7fffdd,23},{141,16#7fffde,23},{142,16#ffffeb,24},{143,16#7fffdf,23},
     {144,16#ffffec,24},{145,16#ffffed,24},{146,16#3fffd7,22},{147,16#7fffe0,23},
     {148,16#ffffee,24},{149,16#7fffe1,23},{150,16#7fffe2,23},{151,16#7fffe3,23},
     {152,16#7fffe4,23},{153,16#1fffdc,21},{154,16#3fffd8,22},{155,16#7fffe5,23},
     {156,16#3fffd9,22},{157,16#7fffe6,23},{158,16#7fffe7,23},{159,16#ffffef,24},
     {160,16#3fffda,22},{161,16#1fffdd,21},{162,16#fffe9,20},{163,16#3fffdb,22},
     {164,16#3fffdc,22},{165,16#7fffe8,23},{166,16#7fffe9,23},{167,16#1fffde,21},
     {168,16#7fffea,23},{169,16#3fffdd,22},{170,16#3fffde,22},{171,16#fffff0,24},
     {172,16#1fffdf,21},{173,16#3fffdf,22},{174,16#7fffeb,23},{175,16#7fffec,23},
     {176,16#1fffe0,21},{177,16#1fffe1,21},{178,16#3fffe0,22},{179,16#1fffe2,21},
     {180,16#7fffed,23},{181,16#3fffe1,22},{182,16#7fffee,23},{183,16#7fffef,23},
     {184,16#fffea,20},{185,16#3fffe2,22},{186,16#3fffe3,22},{187,16#3fffe4,22},
     {188,16#7ffff0,23},{189,16#3fffe5,22},{190,16#3fffe6,22},{191,16#7ffff1,23},
     {192,16#3ffffe0,26},{193,16#3ffffe1,26},{194,16#fffeb,20},{195,16#7fff1,19},
     {196,16#3fffe7,22},{197,16#7ffff2,23},{198,16#3fffe8,22},{199,16#1ffffec,25},
     {200,16#3ffffe2,26},{201,16#3ffffe3,26},{202,16#3ffffe4,26},{203,16#7ffffde,27},
     {204,16#7ffffdf,27},{205,16#3ffffe5,26},{206,16#fffff1,24},{207,16#1ffffed,25},
     {208,16#7fff2,19},{209,16#1fffe3,21},{210,16#3ffffe6,26},{211,16#7ffffe0,27},
     {212,16#7ffffe1,27},{213,16#3ffffe7,26},{214,16#7ffffe2,27},{215,16#fffff2,24},
     {216,16#1fffe4,21},{217,16#1fffe5,21},{218,16#3ffffe8,26},{219,16#3ffffe9,26},
     {220,16#ffffffd,28},{221,16#7ffffe3,27},{222,16#7ffffe4,27},{223,16#7ffffe5,27},
     {224,16#fffec,20},{225,16#fffff3,24},{226,16#fffed,20},{227,16#1fffe6,21},
     {228,16#3fffe9,22},{229,16#1fffe7,21},{230,16#1fffe8,21},{231,16#7ffff3,23},
     {232,16#3fffea,22},{233,16#3fffeb,22},{234,16#1ffffee,25},{235,16#1ffffef,25},
     {236,16#fffff4,24},{237,16#fffff5,24},{238,16#3ffffea,26},{239,16#7ffff4,23},
     {240,16#3ffffeb,26},{241,16#7ffffe6,27},{242,16#3ffffec,26},{243,16#3ffffed,26},
     {244,16#7ffffe7,27},{245,16#7ffffe8,27},{246,16#7ffffe9,27},{247,16#7ffffea,27},
     {248,16#7ffffeb,27},{249,16#ffffffe,28},{250,16#7ffffec,27},{251,16#7ffffed,27},
     {252,16#7ffffee,27},{253,16#7ffffef,27},{254,16#7fffff0,27},{255,16#3ffffee,26},
     {256,16#3fffffff,30}].
