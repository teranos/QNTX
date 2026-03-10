(* Stitcher — weaves conversation turns into a commit narrative
 *
 * Input: JSON-encoded attestation (the PostToolUse that triggered the watcher)
 * Output: JSON response with success/error
 *
 * The triggering attestation contains the branch name in subjects
 * and the git commit command in attributes. From this we extract:
 * - which branch the commit landed on
 * - the commit message (from tool_response.stdout)
 *
 * TODO: query ATSStoreService for conversation turns on this branch
 * since the previous commit, then stitch them into a coherent block.
 *)

let extract_branch payload =
  try
    let json = Yojson.Safe.from_string payload in
    match json with
    | `Assoc fields ->
      (match List.assoc_opt "subjects" fields with
       | Some (`List ((`String branch) :: _)) -> Some branch
       | _ -> None)
    | _ -> None
  with Yojson.Json_error _ -> None

let stitch payload =
  let branch = extract_branch payload in
  let branch_str = match branch with Some b -> b | None -> "unknown" in
  Printf.printf "[loom] Stitching for branch: %s\n%!" branch_str;
  Printf.sprintf {|{"success":true,"result":{"branch":"%s","status":"stub"}}|} branch_str
