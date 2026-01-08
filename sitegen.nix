{ pkgs }:

let
  inherit (pkgs) lib;

  # Static assets
  cssFiles = {
    core = ./web/css/core.css;
    utilities = ./web/css/utilities.css;
    docs = ./web/css/docs.css;
  };
  jsFiles = {
    search = ./web/js/search.js;
  };
  logo = ./web/qntx.jpg;

  # Use Nix's filesystem library to discover markdown files
  docsDir = ./docs;
  allFiles = lib.filesystem.listFilesRecursive docsDir;
  markdownFiles =
    let filtered = lib.filter (path: lib.hasSuffix ".md" (toString path)) allFiles;
    in if filtered == [ ]
    then throw "No markdown files found in docs/ directory"
    else filtered;

  # Calculate relative path from docs/ directory
  getRelativePath = path:
    lib.removePrefix "${toString docsDir}/" (toString path);

  # SEG symbol mappings for semantic navigation
  # Based on docs/development/design-philosophy.md
  categoryMeta = {
    "getting-started" = { symbol = "⍟"; desc = "Entry points"; };
    "architecture" = { symbol = "⌬"; desc = "System design"; };
    "development" = { symbol = "⨳"; desc = "Workflows"; };
    "types" = { symbol = "≡"; desc = "Type reference"; };
    "api" = { symbol = "⋈"; desc = "API reference"; };
    "testing" = { symbol = "✦"; desc = "Test guides"; };
    "security" = { symbol = "+"; desc = "Security"; };
    "vision" = { symbol = "⟶"; desc = "Future direction"; };
    "_root" = { symbol = ""; desc = ""; };
  };

  # Get category metadata with fallback
  getCategoryMeta = cat:
    categoryMeta.${cat} or { symbol = ""; desc = ""; };

  # Create structured file info for each markdown file
  mkFileInfo = mdPath:
    let
      relPath = getRelativePath mdPath;
      dir = dirOf relPath;
      name = lib.removeSuffix ".md" (baseNameOf relPath);
      htmlPath = lib.removeSuffix ".md" relPath + ".html";
      depth = lib.length (lib.filter (x: x != "") (lib.splitString "/" (if dir == "." then "" else dir)));
      prefix = if depth == 0 then "." else lib.concatStringsSep "/" (lib.genList (_: "..") depth);
    in
    {
      inherit mdPath relPath dir name htmlPath depth prefix;
    };

  # Process all files to get structured info
  fileInfos = map mkFileInfo markdownFiles;

  # Group files by directory for index generation
  groupedFiles = lib.groupBy (f: if f.dir == "." then "_root" else (lib.head (lib.splitString "/" f.dir))) fileInfos;

  # HTML escaping function to prevent XSS from malicious filenames
  escapeHtml = s: builtins.replaceStrings
    [ "<" ">" "&" "\"" "'" ]
    [ "&lt;" "&gt;" "&amp;" "&quot;" "&#39;" ]
    s;

  # Convert kebab-case to Title Case
  toTitleCase = s:
    let
      words = lib.splitString "-" s;
      capitalize = w: lib.toUpper (lib.substring 0 1 w) + lib.substring 1 (lib.stringLength w) w;
    in
    lib.concatStringsSep " " (map capitalize words);

  # HTML template - head section
  htmlHead = title: prefix: ''
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>${escapeHtml title}</title>
        <link rel="icon" type="image/jpeg" href="${prefix}/qntx.jpg">
        <link rel="stylesheet" href="${prefix}/css/core.css">
        <link rel="stylesheet" href="${prefix}/css/docs.css">
    </head>
  '';

  # Document page structure
  docBody = prefix: ''
    <body>
        <nav class="doc-nav"><a href="${prefix}/index.html"><img src="${prefix}/qntx.jpg" alt="QNTX" class="site-logo">Documentation Home</a></nav>
  '';

  # Index page structure
  indexBody = ''
    <body>
        <div class="doc-header">
            <img src="./qntx.jpg" alt="QNTX Logo">
            <h1>QNTX Documentation</h1>
        </div>
        <div class="search-container">
            <input type="search" id="search-input" class="search-input" placeholder="Search documentation..." aria-label="Search documentation">
            <div id="search-results" class="search-results" hidden></div>
        </div>
  '';

  # Category order for consistent display
  categoryOrder = [ "getting-started" "architecture" "development" "types" "api" "security" "testing" "vision" ];

  # Generate index section with SEG symbol
  genIndexSection = category: files:
    let
      sortedFiles = lib.sort (a: b: a.name < b.name) files;
      meta = getCategoryMeta category;
      categoryTitle = toTitleCase category;
      symbolHtml = if meta.symbol != "" then ''<span class="category-symbol">${meta.symbol}</span>'' else "";
      descHtml = if meta.desc != "" then ''<span class="category-desc">${escapeHtml meta.desc}</span>'' else "";
      fileLinks = map (f: ''            <li><a href="${f.htmlPath}"><span class="doc-name">${escapeHtml (toTitleCase f.name)}</span></a></li>'') sortedFiles;
    in
    ''
        <section class="category-section">
            <div class="category-header">
                ${symbolHtml}<h2 class="category-title">${escapeHtml categoryTitle}</h2>${descHtml}
            </div>
            <ul class="doc-list">
    ${lib.concatStringsSep "\n" fileLinks}
            </ul>
        </section>'';

  # Generate full index content
  indexContent =
    let
      rootFiles = groupedFiles._root or [ ];
      # Sort categories by defined order, then alphabetically for any extras
      sortedCategories =
        let
          orderedCats = lib.filter (c: lib.hasAttr c groupedFiles) categoryOrder;
          extraCats = lib.filter (c: c != "_root" && !(lib.elem c categoryOrder)) (lib.attrNames groupedFiles);
        in
        orderedCats ++ (lib.sort (a: b: a < b) extraCats);
      rootSection = if rootFiles != [ ] then genIndexSection "_root" rootFiles else "";
      categorySections = map (cat: genIndexSection cat groupedFiles.${cat}) sortedCategories;
    in
    htmlHead "QNTX Documentation" "." +
    indexBody +
    rootSection +
    lib.concatStringsSep "\n" categorySections +
    ''
        <script src="./js/search.js"></script>
    </body>
    </html>'';

  # Create a separate derivation for each markdown file (enables incremental rebuilds)
  mkHtmlDerivation = fileInfo:
    let
      # Read markdown content and rewrite .md links to .html (pure Nix)
      mdContent = builtins.readFile fileInfo.mdPath;
      rewrittenMd = builtins.replaceStrings [ ".md)" ] [ ".html)" ] mdContent;
    in
    pkgs.runCommand "qntx-doc-${fileInfo.name}"
      {
        nativeBuildInputs = [ pkgs.pulldown-cmark ];
      }
      ''
        mkdir -p "$out/$(dirname "${fileInfo.htmlPath}")"
        {
          cat <<'EOF'
${htmlHead "QNTX - ${fileInfo.name}" fileInfo.prefix}
${docBody fileInfo.prefix}
EOF
          cat <<'EOF' | ${pkgs.pulldown-cmark}/bin/pulldown-cmark -T -S -F
${rewrittenMd}
EOF
          echo "    </body>"
          echo "</html>"
        } > "$out/${fileInfo.htmlPath}"
      '';

  # Generate all HTML file derivations
  htmlDerivations = map mkHtmlDerivation fileInfos;

  # Generate search index JSON
  # Escape for JSON string values
  escapeJson = s: builtins.replaceStrings
    [ "\\" "\"" "\n" "\r" "\t" ]
    [ "\\\\" "\\\"" "\\n" "\\r" "\\t" ]
    s;

  # Strip markdown syntax for plain text content
  stripMarkdown = s:
    let
      # Remove code blocks
      noCodeBlocks = builtins.replaceStrings [ "```" ] [ "" ] s;
      # Remove headers markers
      noHeaders = builtins.replaceStrings [ "# " "## " "### " "#### " ] [ "" "" "" "" ] noCodeBlocks;
      # Remove emphasis markers
      noEmphasis = builtins.replaceStrings [ "**" "__" "*" "_" "`" ] [ "" "" "" "" "" ] noHeaders;
      # Remove link syntax [text](url) -> text (simplified)
      noLinks = builtins.replaceStrings [ "[" "](" ] [ "" " " ] noEmphasis;
    in
    noLinks;

  # Generate search index entry for a file
  mkSearchEntry = fileInfo:
    let
      mdContent = builtins.readFile fileInfo.mdPath;
      # Get first 500 chars of stripped content for search
      strippedContent = stripMarkdown mdContent;
      truncatedContent = lib.substring 0 500 strippedContent;
      category = if fileInfo.dir == "." then "General" else toTitleCase (lib.head (lib.splitString "/" fileInfo.dir));
    in
    ''{"title":"${escapeJson (toTitleCase fileInfo.name)}","path":"${fileInfo.htmlPath}","category":"${escapeJson category}","content":"${escapeJson truncatedContent}"}'';

  searchIndexContent = "[" + lib.concatStringsSep "," (map mkSearchEntry fileInfos) + "]";

  searchIndexFile = pkgs.writeTextFile {
    name = "qntx-docs-search-index";
    text = searchIndexContent;
    destination = "/search-index.json";
  };

  # Static assets using linkFarm (pure Nix)
  staticAssets = pkgs.linkFarm "qntx-docs-static" [
    { name = "css/core.css"; path = cssFiles.core; }
    { name = "css/utilities.css"; path = cssFiles.utilities; }
    { name = "css/docs.css"; path = cssFiles.docs; }
    { name = "js/search.js"; path = jsFiles.search; }
    { name = "qntx.jpg"; path = logo; }
  ];

  # Index file as a derivation
  indexFile = pkgs.writeTextFile {
    name = "qntx-docs-index";
    text = indexContent;
    destination = "/index.html";
  };

in
# Compositional assembly: combine static assets, index, search index, and all HTML files
pkgs.symlinkJoin {
  name = "qntx-docs-site";
  paths = [ staticAssets indexFile searchIndexFile ] ++ htmlDerivations;
}
