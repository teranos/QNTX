(* Menhir grammar for AX queries.
 *
 * Top section: token and start declarations.
 * Bottom section (after %%): grammar rules with semantic actions. *)

%token <string> IDENT
%token IS                (* "is" | "are" *)
%token OF                (* "of" | "from" *)
%token BY                (* "by" | "via" *)
%token SINCE UNTIL ON BETWEEN OVER
%token AND
%token SO                (* "so" | "therefore" *)
%token EOF

%start <Ast.ax_query> query

%%

query:
  | subjects = list(IDENT)
    predicates = option(predicate_clause)
    contexts = option(context_clause)
    actors = option(actor_clause)
    temporal = option(temporal_clause)
    actions = option(action_clause)
    EOF
    { { Ast.subjects   = subjects
      ; predicates = (match predicates with Some p -> p | None -> [])
      ; contexts   = (match contexts with Some c -> c | None -> [])
      ; actors     = (match actors with Some a -> a | None -> [])
      ; temporal   = temporal
      ; actions    = (match actions with Some a -> a | None -> [])
      } }

predicate_clause:
  | IS; ps = nonempty_list(IDENT) { ps }

context_clause:
  | OF; cs = nonempty_list(IDENT) { cs }

actor_clause:
  | BY; as_ = nonempty_list(IDENT) { as_ }

temporal_clause:
  | SINCE; d = IDENT              { Ast.Since d }
  | UNTIL; d = IDENT              { Ast.Until d }
  | ON; d = IDENT                 { Ast.On d }
  | BETWEEN; a = IDENT; AND; b = IDENT { Ast.Between (a, b) }
  | OVER; d = IDENT               { Ast.Over d }

action_clause:
  | SO; as_ = nonempty_list(IDENT) { as_ }
