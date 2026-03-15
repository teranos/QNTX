(* Lexer — turns characters into tokens for the parser.
 *
 * sedlex patterns:
 *   'a'..'z'      — character range
 *   Plus(...)     — one or more
 *   white_space   — built-in Unicode whitespace class
 *   eof           — end of input *)

let keyword_or_ident s =
  match String.lowercase_ascii s with
  | "is" | "are"       -> Parser.IS
  | "of" | "from"      -> Parser.OF
  | "by" | "via"       -> Parser.BY
  | "since"            -> Parser.SINCE
  | "until"            -> Parser.UNTIL
  | "on"               -> Parser.ON
  | "between"          -> Parser.BETWEEN
  | "over"             -> Parser.OVER
  | "and"              -> Parser.AND
  | "so" | "therefore" -> Parser.SO
  | _                  -> Parser.IDENT s

let rec token buf =
  match%sedlex buf with
  | white_space -> token buf
  | Plus ('a'..'z' | 'A'..'Z' | '0'..'9' | '_' | '-' | '.' | ':' | '@') ->
    keyword_or_ident (Sedlexing.Utf8.lexeme buf)
  | eof -> Parser.EOF
  | any -> token buf
  | _ -> assert false
