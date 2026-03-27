(* Semantic token classification for AX queries.
 *
 * Mirrors Rust's qntx-core/src/semantic.rs — re-lexes the input and
 * tracks parser state transitions to assign each token its grammatical role.
 * Never fails: classifies whatever tokens exist, suitable for real-time
 * editor feedback on incomplete queries.
 *
 * Token type indices match the LSP SemanticTokensLegend order:
 * 0=keyword, 1=subject, 2=predicate, 3=context, 4=actor,
 * 5=temporal, 6=operator, 7=string, 8=url, 9=unknown *)

type token_type =
  | Keyword
  | Subject
  | Predicate
  | Context
  | Actor
  | Temporal
  | Operator
  | String_
  | Url
  | Unknown

let token_type_index = function
  | Keyword   -> 0
  | Subject   -> 1
  | Predicate -> 2
  | Context   -> 3
  | Actor     -> 4
  | Temporal  -> 5
  | Operator  -> 6
  | String_   -> 7
  | Url       -> 8
  | Unknown   -> 9

type semantic_token = {
  text      : string;
  typ       : token_type;
  offset    : int;
  length    : int;
  is_quoted : bool;
}

(* Classification state — mirrors the parser state machine *)
type classify_state =
  | Subjects
  | Predicates
  | Contexts
  | Actors
  | Temporal_
  | Actions

(* Classify a single lexer token, returning (semantic_type, next_state).
 * Keywords trigger state transitions; identifiers inherit the current state. *)
let classify_one token state =
  match token with
  | Parser.IS       -> (Keyword, Predicates)
  | Parser.OF       -> (Keyword, Contexts)
  | Parser.BY       -> (Keyword, Actors)
  | Parser.SINCE    -> (Keyword, Temporal_)
  | Parser.UNTIL    -> (Keyword, Temporal_)
  | Parser.ON       -> (Keyword, Temporal_)
  | Parser.BETWEEN  -> (Keyword, Temporal_)
  | Parser.OVER     -> (Keyword, Temporal_)
  | Parser.AND      -> (Keyword, state)
  | Parser.SO       -> (Keyword, Actions)
  | Parser.IDENT _  ->
    let typ = match state with
      | Subjects   -> Subject
      | Predicates -> Predicate
      | Contexts   -> Context
      | Actors     -> Actor
      | Temporal_  -> Temporal
      | Actions    -> Unknown
    in
    (typ, state)
  | Parser.EOF      -> (Unknown, state)
  | _               -> (Unknown, state)

(* Classify all tokens in an AX query, returning semantic tokens with positions.
 *
 * Uses sedlex for lexing with position tracking. Each token gets its
 * grammatical role based on parser state transitions. *)
let classify_tokens input =
  let buf = Sedlexing.Utf8.from_string input in
  let tokenizer = Sedlexing.with_tokenizer Lexer.token buf in
  let state = ref Subjects in
  let tokens = ref [] in
  let rec loop_ () =
    let token, start_pos, _end_pos = tokenizer () in
    match token with
    | Parser.EOF -> ()
    | _ ->
      let (typ, next_state) = classify_one token !state in
      state := next_state;
      let offset = start_pos.pos_cnum in
      let text = match token with
        | Parser.IDENT s -> s
        | Parser.IS -> "is"
        | Parser.OF -> "of"
        | Parser.BY -> "by"
        | Parser.SINCE -> "since"
        | Parser.UNTIL -> "until"
        | Parser.ON -> "on"
        | Parser.BETWEEN -> "between"
        | Parser.OVER -> "over"
        | Parser.AND -> "and"
        | Parser.SO -> "so"
        | Parser.EOF -> ""
        | _ -> ""
      in
      let length = String.length text in
      tokens := { text; typ; offset; length; is_quoted = false } :: !tokens;
      loop_ ()
  in
  (try loop_ () with _ -> ());
  List.rev !tokens

(* Serialize semantic tokens to JSON matching Rust output format *)
let token_to_json t =
  `Assoc
    [ ("text",      `String t.text)
    ; ("type",      `Int (token_type_index t.typ))
    ; ("offset",    `Int t.offset)
    ; ("length",    `Int t.length)
    ; ("is_quoted", `Bool t.is_quoted)
    ]

let to_json tokens =
  `List (List.map token_to_json tokens)

(* Encode into LSP delta format: flat array of 5-tuples
 * (deltaLine, deltaStart, length, tokenType, tokenModifiers).
 * AX queries are single-line, so deltaLine is always 0. *)
let encode_lsp tokens =
  let prev_offset = ref 0 in
  let data = ref [] in
  List.iter (fun t ->
    let delta_start = t.offset - !prev_offset in
    data := 0 :: delta_start :: t.length :: token_type_index t.typ :: 0 :: !data;
    prev_offset := t.offset
  ) tokens;
  `List (List.rev_map (fun i -> `Int i) !data)
