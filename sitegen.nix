{ pkgs }:

pkgs.stdenv.mkDerivation {
  pname = "qntx-docs-site";
  version = "dev";
  src = ./docs;

  nativeBuildInputs = [ pkgs.discount ];

  buildPhase = ''
    mkdir -p $out/css

    # Copy CSS files from web/css
    cp ${./web/css/core.css} $out/css/core.css
    cp ${./web/css/utilities.css} $out/css/utilities.css

    # Copy logo and favicon
    cp ${./web/qntx.jpg} $out/qntx.jpg

    # Convert each .md file to HTML
    for md in *.md; do
      [ -f "$md" ] || continue
      name=$(basename "$md" .md)

      # Generate HTML with lowdown
      cat > "$out/$name.html" <<HTML
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>QNTX - $name</title>
    <link rel="icon" type="image/jpeg" href="/qntx.jpg">
    <link rel="stylesheet" href="/css/core.css">
    <link rel="stylesheet" href="/css/utilities.css">
    <style>
        /* Documentation-specific styles */
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
HTML

      ${pkgs.discount}/bin/markdown -f fencedcode,autolink "$md" >> "$out/$name.html"

      echo "</body></html>" >> "$out/$name.html"
    done

    # Generate index
    cat > "$out/index.html" <<HTML
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>QNTX Documentation</title>
    <link rel="icon" type="image/jpeg" href="/qntx.jpg">
    <link rel="stylesheet" href="/css/core.css">
    <link rel="stylesheet" href="/css/utilities.css">
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
HTML

    for md in *.md; do
      [ -f "$md" ] || continue
      name=$(basename "$md" .md)
      echo "      <li><a href=\"$name.html\">$name</a></li>" >> "$out/index.html"
    done

    echo "    </ul></body></html>" >> "$out/index.html"
  '';

  installPhase = "true"; # Files already in $out
}
