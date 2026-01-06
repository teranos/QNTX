{ pkgs }:

let
  inherit (pkgs) lib;

  # Static assets
  cssFiles = {
    core = ./web/css/core.css;
    utilities = ./web/css/utilities.css;
  };
  logo = ./web/qntx.jpg;

  # Use Nix's filesystem library to discover markdown files
  docsDir = ./docs;
  allFiles = lib.filesystem.listFilesRecursive docsDir;
  markdownFiles = lib.filter (path: lib.hasSuffix ".md" (toString path)) allFiles;

  # Calculate relative path from docs/ directory
  getRelativePath = path:
    lib.removePrefix "${toString docsDir}/" (toString path);

  # Create structured file info for each markdown file
  mkFileInfo = mdPath:
    let
      relPath = getRelativePath mdPath;
      dir = dirOf relPath;
      name = lib.removeSuffix ".md" (baseNameOf relPath);
      htmlPath = lib.removeSuffix ".md" relPath + ".html";
      depth = lib.length (lib.filter (x: x != "") (lib.splitString "/" (if dir == "." then "" else dir)));
      prefix = if depth == 0 then "." else lib.concatStringsSep "/" (lib.genList (_: "..") depth);
    in {
      inherit mdPath relPath dir name htmlPath depth prefix;
    };

  # Process all files to get structured info
  fileInfos = map mkFileInfo markdownFiles;

  # Group files by directory for index generation
  groupedFiles = lib.groupBy (f: if f.dir == "." then "_root" else (lib.head (lib.splitString "/" f.dir))) fileInfos;

  # HTML template functions (pure Nix)
  htmlHead = title: prefix: ''
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>${title}</title>
        <link rel="icon" type="image/jpeg" href="${prefix}/qntx.jpg">
        <link rel="stylesheet" href="${prefix}/css/core.css">
        <link rel="stylesheet" href="${prefix}/css/utilities.css">
  '';

  docStyles = prefix: ''
        <style>
            body { max-width: 900px; margin: 0 auto; padding: 40px 20px; }
            pre { background: #f4f4f4; padding: 12px 16px; overflow-x: auto; border-radius: 4px; border: 1px solid #e0e0e0; }
            code { background: #f4f4f4; padding: 2px 6px; border-radius: 3px; font-family: var(--font-mono, monospace); font-size: 0.9em; }
            pre code { background: none; padding: 0; border: none; }
            h1 { border-bottom: 2px solid #e0e0e0; padding-bottom: 10px; }
            h2 { margin-top: 2em; border-bottom: 1px solid #f0f0f0; padding-bottom: 8px; }
            a { color: #0066cc; text-decoration: none; }
            a:hover { text-decoration: underline; }
            nav { margin-bottom: 2em; padding: 12px; background: #f9f9f9; border-radius: 4px; }
            nav a { margin-right: 16px; }
            .site-logo { width: 32px; height: 32px; border-radius: 4px; vertical-align: middle; margin-right: 8px; }
        </style>
    </head>
    <body>
        <nav><a href="${prefix}/index.html"><img src="${prefix}/qntx.jpg" alt="QNTX" class="site-logo">← Documentation Home</a></nav>
  '';

  indexStyles = ''
        <style>
            body { max-width: 900px; margin: 0 auto; padding: 40px 20px; }
            .header { display: flex; align-items: center; margin-bottom: 2em; border-bottom: 2px solid #e0e0e0; padding-bottom: 16px; }
            .header img { width: 48px; height: 48px; border-radius: 6px; margin-right: 16px; }
            h1 { margin: 0; }
            h2 { margin-top: 2em; color: #555; font-size: 1.2em; }
            ul { list-style: none; padding: 0; }
            li { margin: 12px 0; }
            li a { display: block; padding: 12px 16px; background: #f9f9f9; border-radius: 4px; transition: background 0.2s; }
            li a:hover { background: #f0f0f0; }
            a { color: #0066cc; text-decoration: none; }
        </style>
    </head>
    <body>
        <div class="header">
            <img src="./qntx.jpg" alt="QNTX Logo">
            <h1>QNTX Documentation</h1>
        </div>
  '';

  # Generate index HTML structure using pure Nix
  genIndexSection = category: files:
    let
      sortedFiles = lib.sort (a: b: a.name < b.name) files;
      categoryTitle = lib.toUpper (lib.substring 0 1 category) + lib.substring 1 (lib.stringLength category) category;
      fileLinks = map (f: ''          <li><a href="${f.htmlPath}">${f.name}</a></li>'') sortedFiles;
    in ''
        <h2>${categoryTitle}</h2>
        <ul>
${lib.concatStringsSep "\n" fileLinks}
        </ul>'';

  # Generate full index content
  indexContent =
    let
      rootFiles = groupedFiles._root or [];
      categories = lib.filterAttrs (k: v: k != "_root") groupedFiles;
      rootSection = if rootFiles != [] then genIndexSection "Documentation" rootFiles else "";
      categorySections = lib.mapAttrsToList genIndexSection categories;
    in
      htmlHead "QNTX Documentation" "." +
      indexStyles +
      rootSection +
      lib.concatStringsSep "\n" categorySections +
      ''
    </body>
</html>'';

in
pkgs.runCommand "qntx-docs-site"
  {
    nativeBuildInputs = [ pkgs.discount ];
    # Pass file info as JSON to bash for processing
    fileInfoJson = builtins.toJSON fileInfos;
    passAsFile = [ "fileInfoJson" ];
  }
  ''
    # Setup output structure
    mkdir -p $out/css

    # Symlink static assets
    ln -s ${cssFiles.core} $out/css/core.css
    ln -s ${cssFiles.utilities} $out/css/utilities.css
    ln -s ${logo} $out/qntx.jpg

    # Process each markdown file using Nix-computed metadata
    ${pkgs.jq}/bin/jq -c '.[]' $fileInfoJsonPath | while IFS= read -r fileInfo; do
      mdPath=$(echo "$fileInfo" | ${pkgs.jq}/bin/jq -r '.mdPath')
      htmlPath=$(echo "$fileInfo" | ${pkgs.jq}/bin/jq -r '.htmlPath')
      name=$(echo "$fileInfo" | ${pkgs.jq}/bin/jq -r '.name')
      prefix=$(echo "$fileInfo" | ${pkgs.jq}/bin/jq -r '.prefix')
      dir=$(dirname "$htmlPath")

      # Create output directory if needed
      [ "$dir" != "." ] && mkdir -p "$out/$dir"

      # Generate HTML page
      {
        cat <<'EOF'
${htmlHead "QNTX - $NAME" "$PREFIX"}
${docStyles "$PREFIX"}
EOF
        ${pkgs.discount}/bin/markdown -f fencedcode,autolink "$mdPath"
        echo "    </body>"
        echo "</html>"
      } | sed "s|\\\$NAME|$name|g; s|\\\$PREFIX|$prefix|g" > "$out/$htmlPath"
    done

    # Write index.html using Nix-generated content
    cat > $out/index.html <<'EOF'
${indexContent}
EOF
  ''
