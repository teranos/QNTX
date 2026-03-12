open Qntx_loom

let make_payload ?(context="session:test-abc-123") ~branch ~predicate ~text () =
  let attr_key = match predicate with
    | "UserPromptSubmit" -> "prompt"
    | "Stop" -> "last_assistant_message"
    | _ -> "unknown"
  in
  Printf.sprintf
    {|{"subjects":["%s"],"predicates":["%s"],"contexts":["%s"],"attributes":{"%s":"%s"}}|}
    branch predicate context attr_key text

(* Clear global buffer state between tests *)
let reset () = Hashtbl.clear Stitcher.buffers

(* --- word_count --- *)

let test_word_count_empty () =
  Alcotest.(check int) "empty" 0 (Stitcher.word_count "")

let test_word_count_single () =
  Alcotest.(check int) "single word" 1 (Stitcher.word_count "hello")

let test_word_count_multiple () =
  Alcotest.(check int) "three words" 3 (Stitcher.word_count "one two three")

let test_word_count_newlines () =
  Alcotest.(check int) "newline separators" 3 (Stitcher.word_count "one\ntwo\nthree")

let test_word_count_mixed () =
  Alcotest.(check int) "mixed separators" 4 (Stitcher.word_count "one two\nthree four")

(* --- JSON extraction --- *)

let test_extract_branch () =
  let json = Yojson.Safe.from_string {|{"subjects":["main"]}|} in
  Alcotest.(check (option string)) "branch" (Some "main") (Stitcher.extract_branch json)

let test_extract_branch_missing () =
  let json = Yojson.Safe.from_string {|{"subjects":[]}|} in
  Alcotest.(check (option string)) "empty subjects" None (Stitcher.extract_branch json)

let test_extract_branch_no_field () =
  let json = Yojson.Safe.from_string {|{}|} in
  Alcotest.(check (option string)) "no subjects field" None (Stitcher.extract_branch json)

let test_extract_predicate () =
  let json = Yojson.Safe.from_string {|{"predicates":["UserPromptSubmit"]}|} in
  Alcotest.(check (option string)) "predicate" (Some "UserPromptSubmit") (Stitcher.extract_predicate json)

let test_extract_context () =
  let json = Yojson.Safe.from_string {|{"contexts":["session:abc-123"]}|} in
  Alcotest.(check (option string)) "context" (Some "session:abc-123") (Stitcher.extract_context json)

let test_extract_context_missing () =
  let json = Yojson.Safe.from_string {|{"contexts":[]}|} in
  Alcotest.(check (option string)) "empty contexts" None (Stitcher.extract_context json)

let test_extract_context_no_field () =
  let json = Yojson.Safe.from_string {|{}|} in
  Alcotest.(check (option string)) "no contexts field" None (Stitcher.extract_context json)

let test_extract_text_prompt () =
  let json = Yojson.Safe.from_string {|{"attributes":{"prompt":"hello world"}}|} in
  Alcotest.(check (option string)) "prompt text" (Some "hello world") (Stitcher.extract_text json "UserPromptSubmit")

let test_extract_text_assistant () =
  let json = Yojson.Safe.from_string {|{"attributes":{"last_assistant_message":"response here"}}|} in
  Alcotest.(check (option string)) "assistant text" (Some "response here") (Stitcher.extract_text json "Stop")

let test_extract_text_unknown_predicate () =
  let json = Yojson.Safe.from_string {|{"attributes":{"prompt":"hello"}}|} in
  Alcotest.(check (option string)) "unknown predicate" None (Stitcher.extract_text json "Unknown")

(* --- stitch: buffering --- *)

let test_stitch_buffers_single_turn () =
  reset ();
  let payload = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text:"hello world" () in
  let result = Stitcher.stitch payload in
  Alcotest.(check string) "branch" "main" result.branch;
  Alcotest.(check string) "context" "session:test-abc-123" result.context;
  Alcotest.(check (option string)) "no emission" None result.emitted;
  Alcotest.(check bool) "has buffered words" true (result.buffered_words > 0)

let test_stitch_labels_human () =
  reset ();
  let payload = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text:"test" () in
  let _ = Stitcher.stitch payload in
  let entry = Hashtbl.find Stitcher.buffers "main:session:test-abc-123" in
  Alcotest.(check string) "human label" "[human] test" (List.hd entry.turns)

let test_stitch_labels_assistant () =
  reset ();
  let payload = make_payload ~branch:"main" ~predicate:"Stop" ~text:"response" () in
  let _ = Stitcher.stitch payload in
  let entry = Hashtbl.find Stitcher.buffers "main:session:test-abc-123" in
  Alcotest.(check string) "assistant label" "[assistant] response" (List.hd entry.turns)

let test_stitch_separate_branches () =
  reset ();
  let p1 = make_payload ~branch:"feat/a" ~predicate:"UserPromptSubmit" ~text:"hello" () in
  let p2 = make_payload ~branch:"feat/b" ~predicate:"UserPromptSubmit" ~text:"world" () in
  let _ = Stitcher.stitch p1 in
  let _ = Stitcher.stitch p2 in
  Alcotest.(check int) "two branches" 2 (Hashtbl.length Stitcher.buffers)

let test_stitch_context_fallback () =
  reset ();
  let payload = {|{"subjects":["main"],"predicates":["UserPromptSubmit"],"attributes":{"prompt":"hello"}}|} in
  let result = Stitcher.stitch payload in
  Alcotest.(check string) "fallback context" "_" result.context

(* --- stitch: emission --- *)

let long_text n =
  List.init n (fun i -> Printf.sprintf "word%d" i) |> String.concat " "

let test_stitch_emits_at_threshold () =
  reset ();
  let text = long_text 110 in
  let payload = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text () in
  let result = Stitcher.stitch payload in
  Alcotest.(check bool) "emitted" true (Option.is_some result.emitted);
  Alcotest.(check int) "turn count" 1 result.turn_count;
  Alcotest.(check int) "buffer cleared" 0 result.buffered_words

let test_stitch_no_emit_below_threshold () =
  reset ();
  let text = long_text 50 in
  let payload = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text () in
  let result = Stitcher.stitch payload in
  Alcotest.(check bool) "not emitted" true (Option.is_none result.emitted)

let test_stitch_accumulates_to_threshold () =
  reset ();
  let text = long_text 60 in
  let p1 = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text () in
  let p2 = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text () in
  let r1 = Stitcher.stitch p1 in
  let r2 = Stitcher.stitch p2 in
  Alcotest.(check bool) "first: buffered" true (Option.is_none r1.emitted);
  Alcotest.(check bool) "second: emitted" true (Option.is_some r2.emitted);
  Alcotest.(check int) "two turns" 2 r2.turn_count


let test_stitch_emission_order () =
  reset ();
  let p1 = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text:(long_text 60) () in
  let p2 = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text:(long_text 60) () in
  let _ = Stitcher.stitch p1 in
  let r = Stitcher.stitch p2 in
  match r.emitted with
  | None -> Alcotest.fail "expected emission"
  | Some block ->
    (* Human turn should come before assistant turn — chronological order *)
    let human_pos = String.length "[human]" in
    let has_human_first = String.length block > human_pos
      && String.sub block 0 7 = "[human]" in
    Alcotest.(check bool) "human first" true has_human_first

let test_stitch_clears_buffer_after_emit () =
  reset ();
  let text = long_text 110 in
  let payload = make_payload ~branch:"main" ~predicate:"UserPromptSubmit" ~text () in
  let _ = Stitcher.stitch payload in
  Alcotest.(check bool) "buffer removed" true
    (Hashtbl.find_opt Stitcher.buffers "main:session:test-abc-123" = None)

(* --- flush_all --- *)

let test_flush_all_drains_buffers () =
  reset ();
  let p1 = make_payload ~branch:"feat/a" ~predicate:"UserPromptSubmit" ~text:"hello world" () in
  let p2 = make_payload ~branch:"feat/b" ~predicate:"UserPromptSubmit" ~text:"goodbye world" () in
  let _ = Stitcher.stitch p1 in
  let _ = Stitcher.stitch p2 in
  Alcotest.(check int) "two branches buffered" 2 (Hashtbl.length Stitcher.buffers);
  let results = Stitcher.flush_all () in
  Alcotest.(check int) "two results" 2 (List.length results);
  Alcotest.(check int) "buffers empty" 0 (Hashtbl.length Stitcher.buffers);
  List.iter (fun (r : Stitcher.stitch_result) ->
    Alcotest.(check bool) "emitted" true (Option.is_some r.emitted)
  ) results

let test_flush_all_empty () =
  reset ();
  let results = Stitcher.flush_all () in
  Alcotest.(check int) "no results" 0 (List.length results)

(* --- stitch: malformed input --- *)

let test_stitch_malformed_json () =
  reset ();
  let result = Stitcher.stitch "not json" in
  Alcotest.(check string) "unknown branch" "unknown" result.branch;
  Alcotest.(check string) "fallback context" "_" result.context;
  Alcotest.(check (option string)) "no emission" None result.emitted

let test_stitch_missing_text () =
  reset ();
  let payload = {|{"subjects":["main"],"predicates":["UserPromptSubmit"],"contexts":["session:x"],"attributes":{}}|} in
  let result = Stitcher.stitch payload in
  Alcotest.(check string) "branch parsed" "main" result.branch;
  Alcotest.(check string) "context parsed" "session:x" result.context;
  Alcotest.(check (option string)) "no emission" None result.emitted;
  Alcotest.(check int) "nothing buffered" 0 result.buffered_words

(* --- result_to_json --- *)

let test_result_to_json_buffered () =
  let r = Stitcher.{ branch = "main"; context = "session:x"; buffered_words = 42; emitted = None; turn_count = 0 } in
  let json = Yojson.Safe.from_string (Stitcher.result_to_json r) in
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "success" fields with
     | Some (`Bool true) -> ()
     | _ -> Alcotest.fail "expected success:true")
  | _ -> Alcotest.fail "expected JSON object"

let test_result_to_json_emitted () =
  let r = Stitcher.{ branch = "main"; context = "session:x"; buffered_words = 0; emitted = Some "hello world"; turn_count = 1 } in
  let json = Yojson.Safe.from_string (Stitcher.result_to_json r) in
  match json with
  | `Assoc fields ->
    (match List.assoc_opt "result" fields with
     | Some (`Assoc result_fields) ->
       (match List.assoc_opt "emitted" result_fields with
        | Some (`String s) -> Alcotest.(check string) "emitted text" "hello world" s
        | _ -> Alcotest.fail "expected emitted string")
     | _ -> Alcotest.fail "expected result object")
  | _ -> Alcotest.fail "expected JSON object"

let test_result_to_json_escapes_branch () =
  let r = Stitcher.{ branch = {|feat/"quoted"|}; context = "session:x"; buffered_words = 10; emitted = None; turn_count = 0 } in
  let json_str = Stitcher.result_to_json r in
  (* Must parse as valid JSON — would fail if branch wasn't escaped *)
  let _json = Yojson.Safe.from_string json_str in
  ()

(* --- test runner --- *)

let () =
  Alcotest.run "stitcher" [
    "word_count", [
      Alcotest.test_case "empty" `Quick test_word_count_empty;
      Alcotest.test_case "single" `Quick test_word_count_single;
      Alcotest.test_case "multiple" `Quick test_word_count_multiple;
      Alcotest.test_case "newlines" `Quick test_word_count_newlines;
      Alcotest.test_case "mixed" `Quick test_word_count_mixed;
    ];
    "extraction", [
      Alcotest.test_case "branch" `Quick test_extract_branch;
      Alcotest.test_case "branch missing" `Quick test_extract_branch_missing;
      Alcotest.test_case "branch no field" `Quick test_extract_branch_no_field;
      Alcotest.test_case "predicate" `Quick test_extract_predicate;
      Alcotest.test_case "context" `Quick test_extract_context;
      Alcotest.test_case "context missing" `Quick test_extract_context_missing;
      Alcotest.test_case "context no field" `Quick test_extract_context_no_field;
      Alcotest.test_case "prompt text" `Quick test_extract_text_prompt;
      Alcotest.test_case "assistant text" `Quick test_extract_text_assistant;
      Alcotest.test_case "unknown predicate" `Quick test_extract_text_unknown_predicate;
    ];
    "buffering", [
      Alcotest.test_case "single turn" `Quick test_stitch_buffers_single_turn;
      Alcotest.test_case "human label" `Quick test_stitch_labels_human;
      Alcotest.test_case "assistant label" `Quick test_stitch_labels_assistant;
      Alcotest.test_case "separate branches" `Quick test_stitch_separate_branches;
      Alcotest.test_case "context fallback" `Quick test_stitch_context_fallback;
    ];
    "emission", [
      Alcotest.test_case "at threshold" `Quick test_stitch_emits_at_threshold;
      Alcotest.test_case "below threshold" `Quick test_stitch_no_emit_below_threshold;
      Alcotest.test_case "accumulates" `Quick test_stitch_accumulates_to_threshold;
      Alcotest.test_case "chronological order" `Quick test_stitch_emission_order;
      Alcotest.test_case "clears buffer" `Quick test_stitch_clears_buffer_after_emit;
    ];
    "flush_all", [
      Alcotest.test_case "drains buffers" `Quick test_flush_all_drains_buffers;
      Alcotest.test_case "empty noop" `Quick test_flush_all_empty;
    ];
    "malformed", [
      Alcotest.test_case "bad json" `Quick test_stitch_malformed_json;
      Alcotest.test_case "missing text" `Quick test_stitch_missing_text;
    ];
    "result_to_json", [
      Alcotest.test_case "buffered" `Quick test_result_to_json_buffered;
      Alcotest.test_case "emitted" `Quick test_result_to_json_emitted;
      Alcotest.test_case "escapes branch" `Quick test_result_to_json_escapes_branch;
    ];
  ]
