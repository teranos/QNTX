{ pkgs
, gitRevision ? "unknown"
, gitShortRev ? "unknown"
, gitTag ? null
, buildDate ? null
, ciUser ? null
, ciPipeline ? null
, ciRunId ? null
  # Nix infrastructure metadata (passed from flake)
, nixPackages ? [ ]
, nixApps ? [ ]
, nixContainers ? [ ]
}:

let
  inherit (pkgs) lib;

  # ============================================================================
  # Configuration
  # ============================================================================

  githubRepo = "teranos/QNTX";

  provenance = {
    commit = gitShortRev;
    fullCommit = gitRevision;
    tag = gitTag;
    generator = "sitegen.nix";
    date = buildDate;
    user = ciUser;
    pipeline = ciPipeline;
    runId = ciRunId;
  };

  # Static assets as attrsets (single source of truth)
  cssFiles = {
    core = ./web/css/core.css;
    utilities = ./web/css/utilities.css;
    docs = ./web/css/docs.css;
  };

  jsFiles = {
    releases = ./web/js/releases.js;
  };

  logo = ./web/qntx.jpg;

  # SEG symbol mappings for semantic navigation
  categoryMeta = {
    getting-started = { symbol = "⍟"; desc = "Entry points"; };
    architecture = { symbol = "⌬"; desc = "System design"; };
    development = { symbol = "⨳"; desc = "Workflows"; };
    types = { symbol = "≡"; desc = "Type reference"; };
    api = { symbol = "⋈"; desc = "API reference"; };
    testing = { symbol = "✦"; desc = "Test guides"; };
    security = { symbol = "+"; desc = "Security"; };
    vision = { symbol = "⟶"; desc = "Future direction"; };
    _root = { symbol = ""; desc = ""; };
  };

  categoryOrder = [
    "getting-started"
    "architecture"
    "development"
    "types"
    "api"
    "security"
    "testing"
    "vision"
  ];

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
  # Markdown Discovery
  # ============================================================================

  docsDir = ./docs;

  markdownFiles =
    let
      allFiles = lib.filesystem.listFilesRecursive docsDir;
      filtered = lib.filter (p: lib.hasSuffix ".md" (toString p)) allFiles;
    in
    if filtered == [ ]
    then throw "No markdown files found in docs/ directory"
    else filtered;

  getRelativePath = path:
    lib.removePrefix "${toString docsDir}/" (toString path);

  mkFileInfo = mdPath:
    let
      relPath = getRelativePath mdPath;
      dir = dirOf relPath;
      name = lib.removeSuffix ".md" (baseNameOf relPath);
      htmlPath = lib.removeSuffix ".md" relPath + ".html";
      depth = lib.length (lib.filter (x: x != "") (lib.splitString "/" (if dir == "." then "" else dir)));
      prefix = if depth == 0 then "." else lib.concatStringsSep "/" (lib.genList (_: "..") depth);
    in
    { inherit mdPath relPath dir name htmlPath depth prefix; };

  fileInfos = map mkFileInfo markdownFiles;

  groupedFiles = lib.groupBy
    (f: if f.dir == "." then "_root" else lib.head (lib.splitString "/" f.dir))
    fileInfos;

  getCategoryMeta = cat: categoryMeta.${cat} or { symbol = ""; desc = ""; };

  # ============================================================================
  # Page Template System
  # ============================================================================

  mkHead = { title, prefix }: ''
    <!DOCTYPE html>
    <html lang="en">
    <head>
      <meta charset="UTF-8">
      <meta name="viewport" content="width=device-width, initial-scale=1.0">
      <title>${html.escape title}</title>
      <link rel="icon" type="image/jpeg" href="${prefix}/qntx.jpg">
      <link rel="stylesheet" href="${prefix}/css/core.css">
      <link rel="stylesheet" href="${prefix}/css/docs.css">
    </head>'';

  mkNav = { prefix }: ''
    <nav class="doc-nav">
      <a href="${prefix}/index.html">
        <img src="${prefix}/qntx.jpg" alt="QNTX" class="site-logo">Documentation Home
      </a>
    </nav>'';

  provenanceFooter =
    let
      commitLink = ''<a href="https://github.com/${githubRepo}/commit/${provenance.fullCommit}">${provenance.commit}</a>'';
      parts = [
        (lib.optionalString (provenance.tag != null) " (${provenance.tag})")
        (lib.optionalString (provenance.date != null) " on ${provenance.date}")
        (lib.optionalString (provenance.user != null) " by ${provenance.user}")
        (
          if provenance.pipeline != null && provenance.runId != null
          then '' via <a href="https://github.com/${githubRepo}/actions/runs/${provenance.runId}">${provenance.pipeline}</a>''
          else lib.optionalString (provenance.pipeline != null) " via ${provenance.pipeline}"
        )
      ];
    in
    ''
      <footer class="site-footer">
        <p class="provenance">Generated by ${provenance.generator} at commit ${commitLink}${lib.concatStrings parts}</p>
      </footer>'';

  mkPage =
    { title
    , content
    , prefix ? "."
    , nav ? true
    , scripts ? [ ]
    }:
    let
      scriptTags = lib.concatMapStringsSep "\n"
        (s: ''<script src="${prefix}/js/${s}"></script>'')
        scripts;
    in
    ''
      ${mkHead { inherit title prefix; }}
      <body>
      ${lib.optionalString nav (mkNav { inherit prefix; })}
      ${content}
      ${provenanceFooter}
      ${scriptTags}
      </body>
      </html>'';

  # ============================================================================
  # Special Pages (single source of truth for quick links)
  # ============================================================================

  specialPages = {
    downloads = { title = "Downloads"; order = 1; };
    infrastructure = { title = "Build Infrastructure"; order = 2; };
    sitegen = { title = "Sitegen"; order = 3; };
  };

  quickLinksHtml =
    let
      sorted = lib.sort (a: b: a.order < b.order)
        (lib.mapAttrsToList (name: meta: meta // { inherit name; }) specialPages);
    in
    lib.concatMapStringsSep "\n"
      (p: ''<a href="./${p.name}.html" class="quick-link">${p.title}</a>'')
      sorted;

  # ============================================================================
  # Component Renderers
  # ============================================================================

  renderPackageRow = pkg: [
    "<code>${html.escape pkg.name}</code>"
    (html.escape pkg.description)
    "<code>nix build .#${html.escape pkg.name}</code>"
  ];

  renderAppRow = app: [
    "<code>${html.escape app.name}</code>"
    (html.escape app.description)
    "<code>nix run .#${html.escape app.name}</code>"
  ];

  renderContainerCard = ctr: ''
    <div class="download-card">
      <div class="download-card-header">
        <span class="download-card-icon">📦</span>
        <span class="download-card-title">${html.escape ctr.name}</span>
      </div>
      <p class="download-card-desc">${html.escape ctr.description}</p>
      <div class="container-details">
        <p><strong>Image:</strong> <code>${html.escape ctr.image}</code></p>
        <p><strong>Architectures:</strong> ${html.escape (lib.concatStringsSep ", " ctr.architectures)}</p>
        ${lib.optionalString (ctr.ports != [ ]) ''<p><strong>Ports:</strong> ${html.escape (lib.concatStringsSep ", " ctr.ports)}</p>''}
      </div>
    </div>'';

  renderDownloadContainerCard = ctr:
    let
      cleanName = lib.pipe ctr.image [
        (lib.splitString "/")
        lib.last
        (lib.splitString ":")
        lib.head
      ];
    in
    ''
      <div class="download-card">
        <div class="download-card-header">
          <span class="download-card-icon">📦</span>
          <span class="download-card-title">${html.escape ctr.name}</span>
        </div>
        <p class="download-card-desc">${html.escape ctr.description}</p>
        <a href="https://github.com/${githubRepo}/pkgs/container/${html.escape cleanName}" class="download-btn">View Image</a>
      </div>'';

  renderCategoryRow = cat:
    let meta = categoryMeta.${cat};
    in [ "<code>${html.escape cat}/</code>" meta.symbol (html.escape meta.desc) ];

  sortedCategories =
    let
      allCats = lib.filter (c: c != "_root") (lib.attrNames categoryMeta);
      ordered = lib.filter (c: lib.elem c allCats) categoryOrder;
      extra = lib.filter (c: !(lib.elem c categoryOrder)) allCats;
    in
    ordered ++ lib.sort (a: b: a < b) extra;

  # ============================================================================
  # Index Page
  # ============================================================================

  genIndexSection = category: files:
    let
      sortedFiles = lib.sort (a: b: a.name < b.name) files;
      meta = getCategoryMeta category;
      categoryTitle = toTitleCase category;
      symbolHtml = lib.optionalString (meta.symbol != "") ''<span class="category-symbol">${meta.symbol}</span>'';
      descHtml = lib.optionalString (meta.desc != "") ''<span class="category-desc">${html.escape meta.desc}</span>'';
      fileLinks = lib.concatMapStringsSep "\n"
        (f: ''<li><a href="${f.htmlPath}"><span class="doc-name">${html.escape (toTitleCase f.name)}</span></a></li>'')
        sortedFiles;
    in
    ''
      <section class="category-section">
        <div class="category-header">
          ${symbolHtml}<h2 class="category-title">${html.escape categoryTitle}</h2>${descHtml}
        </div>
        <ul class="doc-list">
          ${fileLinks}
        </ul>
      </section>'';

  indexContent =
    let
      rootFiles = groupedFiles._root or [ ];
      orderedCats = lib.filter (c: lib.hasAttr c groupedFiles) categoryOrder;
      extraCats = lib.sort (a: b: a < b)
        (lib.filter (c: c != "_root" && !(lib.elem c categoryOrder)) (lib.attrNames groupedFiles));
      allCats = orderedCats ++ extraCats;
      rootSection = lib.optionalString (rootFiles != [ ]) (genIndexSection "_root" rootFiles);
      categorySections = lib.concatMapStringsSep "\n"
        (cat: genIndexSection cat groupedFiles.${cat})
        allCats;
    in
    mkPage {
      title = "QNTX Documentation";
      nav = false;
      scripts = [ "releases.js" ];
      content = ''
        <div class="doc-header">
          <img src="./qntx.jpg" alt="QNTX Logo">
          <h1>QNTX Documentation</h1>
        </div>

        <nav class="quick-links">
          ${quickLinksHtml}
        </nav>

        <section class="download-section quick-download">
          <h2>Quick Download</h2>
          <div id="latest-release">
            <p class="loading">Loading latest release...</p>
          </div>
          <p style="margin-top: 12px;">
            <a href="./downloads.html">View all downloads and installation options</a>
          </p>
        </section>

        ${rootSection}
        ${categorySections}
      '';
    };

  # ============================================================================
  # Downloads Page
  # ============================================================================

  downloadsContent =
    let
      nixInstallSection = html.section {
        title = "Recommended: Install via Nix";
        content = ''
          <p>The easiest way to install QNTX is using the Nix package manager:</p>
          ${html.codeBlock "nix profile install github:${githubRepo}"}
          <p>This installs the latest version and handles all dependencies automatically.</p>
        '';
      };

      releaseSection = html.section {
        title = "Release Downloads";
        content = ''
          <div id="release-downloads">
            <p class="loading">Loading releases...</p>
          </div>
        '';
      };

      containersSection = lib.optionalString (nixContainers != [ ]) (html.section {
        title = "Docker Images";
        content = ''
          <div class="download-cards">
            ${lib.concatMapStringsSep "\n" renderDownloadContainerCard nixContainers}
          </div>
        '';
      });

      sourceSection = html.section {
        title = "Build from Source";
        content = ''
          <p>Clone the repository and build with Go 1.24+:</p>
          ${html.codeBlock ''
            git clone https://github.com/${githubRepo}.git
            cd QNTX
            make build''}
          <p>Or use Nix for reproducible builds:</p>
          ${html.codeBlock "nix build github:${githubRepo}"}
        '';
      };
    in
    mkPage {
      title = "QNTX Downloads";
      scripts = [ "releases.js" ];
      content = ''
        <h1>Download QNTX</h1>
        ${nixInstallSection}
        ${releaseSection}
        ${containersSection}
        ${sourceSection}
      '';
    };

  # ============================================================================
  # Infrastructure Page
  # ============================================================================

  infrastructureContent =
    let
      quickStartSection = html.section {
        title = "Quick Start";
        content = html.codeBlock ''
          # Install Nix (if not already installed)
          curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh

          # Build QNTX
          nix build github:${githubRepo}

          # Enter development shell
          nix develop github:${githubRepo}'';
      };

      packagesSection = lib.optionalString (nixPackages != [ ]) (html.section {
        title = "Packages";
        content = ''
          <p>Available Nix packages that can be built from this flake:</p>
          ${html.table {
            headers = [ "Package" "Description" "Build Command" ];
            rows = map renderPackageRow nixPackages;
          }}
        '';
      });

      appsSection = lib.optionalString (nixApps != [ ]) (html.section {
        title = "Apps";
        content = ''
          <p>Runnable applications for common tasks:</p>
          ${html.table {
            headers = [ "App" "Description" "Run Command" ];
            rows = map renderAppRow nixApps;
          }}
        '';
      });

      containersSection = lib.optionalString (nixContainers != [ ]) (html.section {
        title = "Container Images";
        content = ''
          <p>Docker/OCI container images built with Nix for reproducible deployments:</p>
          <div class="download-cards">
            ${lib.concatMapStringsSep "\n" renderContainerCard nixContainers}
          </div>
        '';
      });

      devShellSection = html.section {
        title = "Development Shell";
        content = ''
          <p>The development shell includes all tools needed to build and test QNTX:</p>
          ${html.codeBlock "nix develop"}
          <p>This provides: Go, Rust, Python, protobuf, SQLite, and pre-commit hooks.</p>
        '';
      };

      reproducibilitySection = html.section {
        title = "Reproducibility";
        content = ''
          <p>All builds are fully reproducible. The same inputs always produce identical outputs:</p>
          <ul>
            <li><strong>Lockfile:</strong> <code>flake.lock</code> pins all dependencies</li>
            <li><strong>Vendor hash:</strong> Go modules are content-addressed</li>
            <li><strong>Binary cache:</strong> <code>qntx.cachix.org</code> for pre-built artifacts</li>
          </ul>
        '';
      };
    in
    mkPage {
      title = "QNTX Infrastructure";
      content = ''
        <h1>Build Infrastructure</h1>
        <p>QNTX uses <a href="https://nixos.org/">Nix</a> for reproducible builds.
           All packages, container images, and development tools are defined in <code>flake.nix</code>.</p>

        ${quickStartSection}
        ${packagesSection}
        ${appsSection}
        ${containersSection}
        ${devShellSection}
        ${reproducibilitySection}
      '';
    };

  # ============================================================================
  # Sitegen Self-Documentation Page
  # ============================================================================

  generatedStructure =
    let
      rootFilesList = [
        { path = "index.html"; desc = "Main documentation index"; }
        { path = "downloads.html"; desc = "Release downloads (GitHub API)"; }
        { path = "infrastructure.html"; desc = "Nix build documentation"; }
        { path = "sitegen.html"; desc = "This page"; }
        { path = "build-info.json"; desc = "Provenance metadata"; }
        { path = "qntx.jpg"; desc = "Logo"; }
      ];

      cssFileList = lib.mapAttrsToList (name: _: { path = "css/${name}.css"; }) cssFiles;
      jsFileList = lib.mapAttrsToList (name: _: { path = "js/${name}.js"; }) jsFiles;
      docHtmlFiles = map (f: { path = f.htmlPath; }) fileInfos;

      docFilesByDir = lib.groupBy (f: dirOf f.path) docHtmlFiles;
      docDirs = lib.filter (d: d != ".") (lib.attrNames docFilesByDir);

      renderEntry = prefix: file:
        let desc = lib.optionalString (file ? desc) "  # ${file.desc}";
        in "${prefix}${baseNameOf file.path}${desc}";

      sortedRootFiles = lib.sort (a: b: a.path < b.path) rootFilesList;
      sortedCssFiles = lib.sort (a: b: a.path < b.path) cssFileList;
      sortedJsFiles = lib.sort (a: b: a.path < b.path) jsFileList;
      sortedDocDirs = lib.sort (a: b: a < b) docDirs;

      rootEntries = lib.imap0
        (i: f:
          let isLast = i == lib.length sortedRootFiles - 1 && sortedDocDirs == [ ];
          in renderEntry (if isLast then "└── " else "├── ") f
        )
        sortedRootFiles;

      cssEntries = [ "├── css/" ] ++ lib.imap0
        (i: f:
          let isLast = i == lib.length sortedCssFiles - 1;
          in renderEntry (if isLast then "│   └── " else "│   ├── ") f
        )
        sortedCssFiles;

      jsEntries = [ "├── js/" ] ++ lib.imap0
        (i: f:
          let isLast = i == lib.length sortedJsFiles - 1;
          in renderEntry (if isLast then "│   └── " else "│   ├── ") f
        )
        sortedJsFiles;

      docDirEntries = lib.concatLists (lib.imap0
        (di: dir:
          let
            isLastDir = di == lib.length sortedDocDirs - 1;
            dirPrefix = if isLastDir then "└── " else "├── ";
            filePrefix = if isLastDir then "    " else "│   ";
            dirFiles = lib.sort (a: b: a.path < b.path) docFilesByDir.${dir};
          in
          [ "${dirPrefix}${dir}/" ] ++ lib.imap0
            (fi: f:
              let isLastFile = fi == lib.length dirFiles - 1;
              in "${filePrefix}${if isLastFile then "└── " else "├── "}${baseNameOf f.path}"
            )
            dirFiles
        )
        sortedDocDirs);
    in
    [ "qntx-docs-site/" ] ++ rootEntries ++ cssEntries ++ jsEntries ++ docDirEntries;

  sitegenContent =
    let
      featuresSection = html.section {
        title = "Features";
        content = html.table {
          headers = [ "Feature" "Description" ];
          rows = [
            [ "<strong>Pure Nix</strong>" "Entire generator written in Nix - no shell scripts, no external build tools" ]
            [ "<strong>Markdown to HTML</strong>" ''Converts <code>docs/*.md</code> to HTML using <a href="https://github.com/raphlinus/pulldown-cmark">pulldown-cmark</a>'' ]
            [ "<strong>SEG Symbol Navigation</strong>" "Categories marked with semantic symbols: ⍟ Getting Started, ⌬ Architecture, ⨳ Development, ≡ Types, ⋈ API" ]
            [ "<strong>Dark Mode</strong>" "Automatic dark/light theme via <code>prefers-color-scheme</code>" ]
            [ "<strong>GitHub Releases</strong>" "Dynamic release downloads fetched client-side from GitHub API" ]
            [ "<strong>Provenance</strong>" "Every page shows commit, tag, date, CI user, and pipeline info" ]
            [ "<strong>Self-documenting Infra</strong>" "Nix packages, apps, and containers documented from flake metadata" ]
            [ "<strong>Incremental Builds</strong>" "Each markdown file is a separate derivation for faster rebuilds" ]
          ];
        };
      };

      structureSection = html.section {
        title = "Generated Structure";
        content = ''
          <p>This structure is generated dynamically from the actual sitegen outputs
             (${toString (lib.length fileInfos)} documentation files discovered):</p>
          ${html.codeBlock (lib.concatStringsSep "\n" generatedStructure)}
        '';
      };

      howItWorksSection = html.section {
        title = "How It Works";
        content = ''
          <h3>1. Markdown Discovery</h3>
          <p>Uses <code>lib.filesystem.listFilesRecursive</code> to find all <code>.md</code> files in <code>docs/</code>:</p>
          ${html.codeBlock ''
            markdownFiles = lib.filter (path: lib.hasSuffix ".md" (toString path))
              (lib.filesystem.listFilesRecursive ./docs);''}

          <h3>2. Per-file Derivations</h3>
          <p>Each markdown file becomes its own Nix derivation, enabling incremental builds:</p>
          ${html.codeBlock ''
            mkHtmlDerivation = fileInfo:
              pkgs.runCommand "qntx-doc-''${fileInfo.name}" {
                nativeBuildInputs = [ pkgs.pulldown-cmark ];
              } '''
                pulldown-cmark input.md > $out/output.html
              ''';''}

          <h3>3. Compositional Assembly</h3>
          <p>All derivations combined with <code>symlinkJoin</code>:</p>
          ${html.codeBlock ''
            pkgs.symlinkJoin {
              name = "qntx-docs-site";
              paths = [ staticAssets ] ++ lib.attrValues outputs ++ htmlDerivations;
            }''}
        '';
      };

      provenanceParamsSection = html.section {
        title = "Provenance Parameters";
        content = ''
          <p>Sitegen accepts these parameters for build provenance:</p>
          ${html.table {
            headers = [ "Parameter" "Type" "Description" ];
            rows = [
              [ "<code>gitRevision</code>" "string" "Full commit hash" ]
              [ "<code>gitShortRev</code>" "string" "Short commit hash (displayed)" ]
              [ "<code>gitTag</code>" "string?" ''Release tag (e.g., "v1.0.0")'' ]
              [ "<code>buildDate</code>" "string?" ''Build date (e.g., "2025-01-08")'' ]
              [ "<code>ciUser</code>" "string?" ''CI actor (e.g., "github-actions")'' ]
              [ "<code>ciPipeline</code>" "string?" "Workflow name" ]
              [ "<code>ciRunId</code>" "string?" "GitHub Actions run ID (links to run)" ]
            ];
          }}
        '';
      };

      infraMetadataSection = html.section {
        title = "Infrastructure Metadata";
        content = ''
          <p>Pass Nix infrastructure metadata to generate the infrastructure page:</p>
          ${html.table {
            headers = [ "Parameter" "Type" "Description" ];
            rows = [
              [ "<code>nixPackages</code>" "[{name, description}]" "List of buildable packages" ]
              [ "<code>nixApps</code>" "[{name, description}]" "List of runnable apps" ]
              [ "<code>nixContainers</code>" "[{name, description, image, architectures, ports}]" "List of container images" ]
            ];
          }}
        '';
      };

      buildingSection = html.section {
        title = "Building the Site";
        content = html.codeBlock ''
          # Build docs site
          nix build .#docs-site

          # Copy to web/site/ for serving
          nix run .#build-docs-site

          # Serve locally (example with Python)
          cd result && python -m http.server 8000'';
      };

      addingDocsSection = html.section {
        title = "Adding Documentation";
        content = ''
          <p>To add new documentation:</p>
          <ol>
            <li>Create a markdown file in <code>docs/</code> (e.g., <code>docs/guides/my-guide.md</code>)</li>
            <li>The file will be automatically discovered and converted to HTML</li>
            <li>Category is determined by the first directory (e.g., <code>guides/</code>)</li>
            <li>Links between docs: use <code>.md</code> extension (auto-rewritten to <code>.html</code>)</li>
          </ol>
        '';
      };

      segMappingSection = html.section {
        title = "SEG Category Mapping";
        content = ''
          <p>Directories are mapped to SEG symbols for semantic navigation
             (${toString (lib.length sortedCategories)} categories defined):</p>
          ${html.table {
            headers = [ "Directory" "Symbol" "Meaning" ];
            rows = map renderCategoryRow sortedCategories;
          }}
        '';
      };
    in
    mkPage {
      title = "QNTX Sitegen";
      content = ''
        <h1>Documentation Generator</h1>
        <p>This documentation site is generated by <code>sitegen.nix</code>, a pure Nix static site generator.
           No external tools or build steps required beyond Nix itself.</p>

        ${featuresSection}
        ${structureSection}
        ${howItWorksSection}
        ${provenanceParamsSection}
        ${infraMetadataSection}
        ${buildingSection}
        ${addingDocsSection}
        ${segMappingSection}
      '';
    };

  # ============================================================================
  # Markdown to HTML Derivations
  # ============================================================================

  mkHtmlDerivation = fileInfo:
    let
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
        ${mkHead { title = "QNTX - ${fileInfo.name}"; prefix = fileInfo.prefix; }}
        <body>
        ${mkNav { prefix = fileInfo.prefix; }}
        EOF
          cat <<'EOF' | ${pkgs.pulldown-cmark}/bin/pulldown-cmark -T -S -F
        ${rewrittenMd}
        EOF
          cat <<'EOF'
        ${provenanceFooter}
        </body>
        </html>
        EOF
        } > "$out/${fileInfo.htmlPath}"
      '';

  htmlDerivations = map mkHtmlDerivation fileInfos;

  # ============================================================================
  # Build Info
  # ============================================================================

  buildInfoContent = builtins.toJSON (lib.filterAttrs (_: v: v != null) {
    generator = provenance.generator;
    commit = provenance.fullCommit;
    shortCommit = provenance.commit;
    tag = provenance.tag;
    date = provenance.date;
    user = provenance.user;
    pipeline = provenance.pipeline;
    runId = provenance.runId;
    repository = "https://github.com/${githubRepo}";
  });

  # ============================================================================
  # Static Assets
  # ============================================================================

  staticAssets = pkgs.linkFarm "qntx-docs-static" (
    lib.mapAttrsToList (name: path: { name = "css/${name}.css"; inherit path; }) cssFiles
    ++ lib.mapAttrsToList (name: path: { name = "js/${name}.js"; inherit path; }) jsFiles
    ++ [{ name = "qntx.jpg"; path = logo; }]
  );

  # ============================================================================
  # Output Derivations
  # ============================================================================

  outputs = {
    "index.html" = pkgs.writeTextFile {
      name = "qntx-docs-index";
      text = indexContent;
      destination = "/index.html";
    };

    "downloads.html" = pkgs.writeTextFile {
      name = "qntx-docs-downloads";
      text = downloadsContent;
      destination = "/downloads.html";
    };

    "infrastructure.html" = pkgs.writeTextFile {
      name = "qntx-docs-infrastructure";
      text = infrastructureContent;
      destination = "/infrastructure.html";
    };

    "sitegen.html" = pkgs.writeTextFile {
      name = "qntx-docs-sitegen";
      text = sitegenContent;
      destination = "/sitegen.html";
    };

    "build-info.json" = pkgs.writeTextFile {
      name = "qntx-docs-build-info";
      text = buildInfoContent;
      destination = "/build-info.json";
    };
  };

in

# ============================================================================
  # Final Assembly
  # ============================================================================

pkgs.symlinkJoin {
  name = "qntx-docs-site";
  paths = [ staticAssets ] ++ lib.attrValues outputs ++ htmlDerivations;
}
