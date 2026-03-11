(* Shim: re-export Google_types.Struct so atsstore.ml's
 * Imported'modules.Struct = Struct resolves correctly *)
include Google_types.Struct
