# sitegen/utils.nix - HTML, text, and date utility functions
{ pkgs, lib }:

{
  # ============================================================================
  # HTML Utilities
  # ============================================================================

  html = rec {
    escape = s: builtins.replaceStrings
      [ "<" ">" "&" "\"" "'" ]
      [ "&lt;" "&gt;" "&amp;" "&quot;" "&#39;" ]
      s;

    escapeJson = s: builtins.replaceStrings
      [ "\\" "\"" "\n" "\r" "\t" ]
      [ "\\\\" "\\\"" "\\n" "\\r" "\\t" ]
      s;

    tag = name: attrs: content:
      let
        attrStr = lib.concatStringsSep " "
          (lib.mapAttrsToList (k: v: ''${k}="${v}"'') attrs);
      in
      "<${name}${lib.optionalString (attrs != { }) " ${attrStr}"}>${content}</${name}>";

    table = { class ? "nix-table", headers, rows }:
      let
        headerRow = "<tr>" + lib.concatMapStringsSep "" (h: "<th>${h}</th>") headers + "</tr>";
        bodyRows = lib.concatMapStringsSep "\n"
          (row:
            "<tr>" + lib.concatMapStringsSep "" (cell: "<td>${cell}</td>") row + "</tr>"
          )
          rows;
      in
      ''
        <table class="${class}">
          <thead>${headerRow}</thead>
          <tbody>
            ${bodyRows}
          </tbody>
        </table>'';

    section = { title, content, class ? "download-section" }: ''
      <section class="${class}">
        <h2>${title}</h2>
        ${content}
      </section>'';

    codeBlock = content: ''<div class="install-code">${content}</div>'';
  };

  # ============================================================================
  # Text Utilities
  # ============================================================================

  toTitleCase = s:
    let
      capitalize = w:
        lib.toUpper (lib.substring 0 1 w) + lib.substring 1 (lib.stringLength w) w;
    in
    lib.concatMapStringsSep " " capitalize (lib.splitString "-" s);

  stripMarkdown = s: lib.pipe s [
    (builtins.replaceStrings [ "```" ] [ "" ])
    (builtins.replaceStrings [ "# " "## " "### " "#### " ] [ "" "" "" "" ])
    (builtins.replaceStrings [ "**" "__" "*" "_" "`" ] [ "" "" "" "" "" ])
    (builtins.replaceStrings [ "[" "](" ] [ "" " " ])
  ];

  # ============================================================================
  # Date Utilities
  # ============================================================================

  # Convert Unix timestamp to RFC 822 format for RSS using shell date utility
  # Example: 1737244800 -> "Mon, 19 Jan 2026 00:00:00 +0000"
  timestampToRfc822 = ts:
    let
      dateFile = pkgs.runCommand "timestamp-to-rfc822-${toString ts}"
        { nativeBuildInputs = [ pkgs.coreutils ]; }
        ''
          date -u -d @${toString ts} '+%a, %d %b %Y %H:%M:%S +0000' > $out
        '';
    in
    lib.trim (builtins.readFile dateFile);

  # Convert Unix timestamp to YYYY-MM-DD format for sitemaps
  timestampToDate = ts:
    let
      dateFile = pkgs.runCommand "timestamp-to-date-${toString ts}"
        { nativeBuildInputs = [ pkgs.coreutils ]; }
        ''
          date -u -d @${toString ts} '+%Y-%m-%d' > $out
        '';
    in
    lib.trim (builtins.readFile dateFile);
}
