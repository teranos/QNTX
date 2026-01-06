{ pkgs }:

let
  # Extract shared CSS as Nix strings for cleaner references
  cssFiles = {
    core = ./web/css/core.css;
    utilities = ./web/css/utilities.css;
  };

  logo = ./web/qntx.jpg;

  # HTML template fragments
  htmlHead = title: ''
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>${title}</title>
        <link rel="icon" type="image/jpeg" href="/qntx.jpg">
        <link rel="stylesheet" href="/css/core.css">
        <link rel="stylesheet" href="/css/utilities.css">
  '';

  docStyles = ''
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
        <nav><a href="/index.html"><img src="/qntx.jpg" alt="QNTX" class="site-logo">← Documentation Home</a></nav>
  '';

  indexStyles = ''
        <style>
            body { max-width: 900px; margin: 0 auto; padding: 40px 20px; }
            .header { display: flex; align-items: center; margin-bottom: 2em; border-bottom: 2px solid #e0e0e0; padding-bottom: 16px; }
            .header img { width: 48px; height: 48px; border-radius: 6px; margin-right: 16px; }
            h1 { margin: 0; }
            ul { list-style: none; padding: 0; }
            li { margin: 12px 0; }
            li a { display: block; padding: 12px 16px; background: #f9f9f9; border-radius: 4px; transition: background 0.2s; }
            li a:hover { background: #f0f0f0; }
            a { color: #0066cc; text-decoration: none; }
        </style>
    </head>
    <body>
        <div class="header">
            <img src="/qntx.jpg" alt="QNTX Logo">
            <h1>QNTX Documentation</h1>
        </div>
        <ul>
  '';

in
pkgs.runCommand "qntx-docs-site"
  {
    nativeBuildInputs = [ pkgs.discount ];
    src = ./docs;

    # Pass template fragments as environment variables
    inherit docStyles indexStyles;
    htmlHeadDoc = htmlHead "QNTX - \$name";
    htmlHeadIndex = htmlHead "QNTX Documentation";
  }
  ''
    # Setup output structure
    mkdir -p $out/css

    # Symlink static assets (more efficient than copying)
    ln -s ${cssFiles.core} $out/css/core.css
    ln -s ${cssFiles.utilities} $out/css/utilities.css
    ln -s ${logo} $out/qntx.jpg

    # Process markdown files
    cd $src
    for md in *.md; do
      [ -f "$md" ] || continue
      name="''${md%.md}"

      # Build HTML page
      {
        echo "$htmlHeadDoc" | sed "s/\\\$name/$name/g"
        echo "$docStyles"
        ${pkgs.discount}/bin/markdown -f fencedcode,autolink "$md"
        echo "    </body>"
        echo "</html>"
      } > "$out/$name.html"
    done

    # Generate index
    {
      echo "$htmlHeadIndex"
      echo "$indexStyles"

      for md in *.md; do
        [ -f "$md" ] || continue
        name="''${md%.md}"
        echo "      <li><a href=\"$name.html\">$name</a></li>"
      done

      echo "        </ul>"
      echo "    </body>"
      echo "</html>"
    } > "$out/index.html"
  ''
