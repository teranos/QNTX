(* Session discovery — scans Claude Code JSONL files and determines session states
 *
 * Scans ~/.claude/projects/ for .jsonl session files, cross-references with
 * ATS weaves and WeaveComplete attestations to determine each session's state:
 *   - unweaved: JSONL on disk, no weaves in ATS
 *   - partial: weaves exist (from Graunde UDP), no WeaveComplete attestation
 *   - complete: WeaveComplete attestation exists, fully imported
 *   - stale: WeaveComplete exists but JSONL grew since import *)

let claude_projects_dir =
  let home = Sys.getenv "HOME" in
  Filename.concat home ".claude/projects"

(* A discovered session with its file metadata *)
type session_file = {
  project : string;        (* project slug, e.g. "-Users-s-b-vanhouten-SBVH-teranos-tmp3-QNTX" *)
  session_id : string;     (* UUID from filename *)
  file_path : string;      (* full path to .jsonl *)
  file_size : int;         (* bytes *)
  line_count : int;        (* number of lines *)
}

type session_state =
  | Unweaved
  | Partial
  | Complete
  | Stale

type session_info = {
  file : session_file;
  state : session_state;
  weave_count : int;       (* number of weaves in ATS for this session *)
}

(* Count lines in a file without reading full content *)
let count_lines path =
  let ic = open_in path in
  let n = ref 0 in
  (try while true do ignore (input_line ic); incr n done
   with End_of_file -> close_in ic);
  !n

(* Scan a project directory for .jsonl session files *)
let scan_project project_dir project_name =
  let entries = try Sys.readdir project_dir with _ -> [||] in
  Array.to_list entries
  |> List.filter_map (fun name ->
    if Filename.check_suffix name ".jsonl" then
      let session_id = Filename.remove_extension name in
      let file_path = Filename.concat project_dir name in
      let file_size = (Unix.stat file_path).st_size in
      let line_count = count_lines file_path in
      Some { project = project_name; session_id; file_path; file_size; line_count }
    else
      None)

(* Scan all projects under ~/.claude/projects/ *)
let scan_all_sessions () =
  let entries = try Sys.readdir claude_projects_dir with _ -> [||] in
  Array.to_list entries
  |> List.filter_map (fun name ->
    let dir = Filename.concat claude_projects_dir name in
    if Sys.is_directory dir then
      Some (scan_project dir name)
    else
      None)
  |> List.concat

(* Extract file_size from a WeaveComplete attestation's attributes *)
let extract_wc_file_size (a : Qntx_plugin_proto.Atsstore.Protocol.Attestation.t) =
  match a.attributes with
  | Some fields ->
    (match List.assoc_opt "file_size" fields with
     | Some (Some (`Number_value n)) -> Some (int_of_float n)
     | _ -> None)
  | None -> None

(* Determine session state by cross-referencing filesystem with ATS.
 *
 * weave_contexts: set of contexts that have at least one Weave in ATS
 * wc_map: session_id → WeaveComplete attestation (if any) *)
let determine_state file ~weave_contexts ~wc_map =
  let context = "session:" ^ file.session_id in
  let has_weaves = Hashtbl.mem weave_contexts context in
  let wc = Hashtbl.find_opt wc_map file.session_id in
  match has_weaves, wc with
  | false, None -> Unweaved
  | true, None -> Partial
  | _, Some wc_att ->
    (* Check staleness: did the file grow since last import? *)
    let recorded_size = extract_wc_file_size wc_att in
    (match recorded_size with
     | Some sz when sz < file.file_size -> Stale
     | _ -> Complete)

let state_to_string = function
  | Unweaved -> "unweaved"
  | Partial -> "partial"
  | Complete -> "complete"
  | Stale -> "stale"

(* Build session info list: scan files, query ATS, determine states.
 * Returns Lwt because ATS queries are async. *)
let discover () =
  let open Lwt.Syntax in
  let files = scan_all_sessions () in

  (* Get all weave contexts from ATS *)
  let* weave_result = Ats_client.get_weaves () in
  let weave_contexts = Hashtbl.create 64 in
  (match weave_result with
   | Ok attestations ->
     List.iter (fun (a : Qntx_plugin_proto.Atsstore.Protocol.Attestation.t) ->
       List.iter (fun ctx -> Hashtbl.replace weave_contexts ctx true) a.contexts
     ) attestations
   | Error _ -> ());

  (* Get all WeaveComplete attestations *)
  let* wc_result = Ats_client.get_weave_completes () in
  let wc_map = Hashtbl.create 32 in
  (match wc_result with
   | Ok attestations ->
     List.iter (fun (a : Qntx_plugin_proto.Atsstore.Protocol.Attestation.t) ->
       (* subject is the session_id *)
       List.iter (fun sid -> Hashtbl.replace wc_map sid a) a.subjects
     ) attestations
   | Error _ -> ());

  (* Count weaves per context *)
  let weave_counts = Hashtbl.create 64 in
  (match weave_result with
   | Ok attestations ->
     List.iter (fun (a : Qntx_plugin_proto.Atsstore.Protocol.Attestation.t) ->
       List.iter (fun ctx ->
         let n = match Hashtbl.find_opt weave_counts ctx with
           | Some n -> n | None -> 0 in
         Hashtbl.replace weave_counts ctx (n + 1)
       ) a.contexts
     ) attestations
   | Error _ -> ());

  let sessions = List.map (fun file ->
    let state = determine_state file ~weave_contexts ~wc_map in
    let context = "session:" ^ file.session_id in
    let weave_count = match Hashtbl.find_opt weave_counts context with
      | Some n -> n | None -> 0 in
    { file; state; weave_count }
  ) files in

  Lwt.return sessions

(* Serialize session list to JSON *)
let sessions_to_json sessions =
  let session_json (s : session_info) =
    `Assoc [
      ("session_id", `String s.file.session_id);
      ("project", `String s.file.project);
      ("file_path", `String s.file.file_path);
      ("file_size", `Int s.file.file_size);
      ("line_count", `Int s.file.line_count);
      ("state", `String (state_to_string s.state));
      ("weave_count", `Int s.weave_count);
    ]
  in
  (* Group by project *)
  let tbl = Hashtbl.create 16 in
  List.iter (fun s ->
    let existing = match Hashtbl.find_opt tbl s.file.project with
      | Some l -> l | None -> [] in
    Hashtbl.replace tbl s.file.project (s :: existing)
  ) sessions;
  let projects = Hashtbl.fold (fun project sessions acc ->
    let sorted = List.sort (fun a b ->
      compare b.file.file_size a.file.file_size  (* largest first *)
    ) sessions in
    (project, `Assoc [
      ("sessions", `List (List.map session_json sorted));
      ("session_count", `Int (List.length sessions));
    ]) :: acc
  ) tbl [] in
  let sorted_projects = List.sort (fun (a, _) (b, _) -> String.compare a b) projects in
  `Assoc [
    ("projects", `Assoc sorted_projects);
    ("project_count", `Int (List.length sorted_projects));
    ("total_sessions", `Int (List.length sessions));
  ]
