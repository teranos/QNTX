{ pkgs
, gitRevision ? "unknown"
, gitShortRev ? "unknown"
, gitTag ? null
, buildDate ? null
, gitCommitDate ? null
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

  # ============================================================================
  # GitHub Releases Fetching (Build Time)
  # ============================================================================

  # Fetch releases from GitHub API using fixed-output derivation
  # This allows network access during build without experimental features
  # Update hash when releases change: set to all zeros, build, copy real hash from error
  releasesJson = pkgs.runCommand "qntx-releases.json"
    {
      nativeBuildInputs = [ pkgs.curl pkgs.cacert ];
      outputHashMode = "flat";
      outputHashAlgo = "sha256";
      outputHash = "sha256-zGd2tzU1n3Y1ih8IeGRL7kJFM8v4MD24ZEmU+imQXQQ=";
    } ''
    curl -s -L -H "Accept: application/vnd.github+json" \
      "https://api.github.com/repos/${githubRepo}/releases" > $out
  '';

  # Parse releases JSON (import from derivation)
  releases = lib.importJSON releasesJson;

  # Helper to determine platform from asset name
  getPlatform = assetName:
    let name = lib.toLower assetName;
    in
    if lib.hasInfix "linux" name && lib.hasInfix "amd64" name then "linux-amd64"
    else if lib.hasInfix "linux" name && (lib.hasInfix "arm64" name || lib.hasInfix "aarch64" name) then "linux-arm64"
    else if (lib.hasInfix "darwin" name || lib.hasInfix "macos" name) && lib.hasInfix "amd64" name then "darwin-amd64"
    else if (lib.hasInfix "darwin" name || lib.hasInfix "macos" name) && (lib.hasInfix "arm64" name || lib.hasInfix "aarch64" name) then "darwin-arm64"
    else if lib.hasInfix "windows" name && lib.hasInfix "amd64" name then "windows-amd64"
    else "other";

  platformNames = {
    "linux-amd64" = "Linux x86_64";
    "linux-arm64" = "Linux ARM64";
    "darwin-amd64" = "macOS Intel";
    "darwin-arm64" = "macOS Apple Silicon";
    "windows-amd64" = "Windows x86_64";
    "other" = "Other";
  };

  # Format file size
  formatSize = bytes:
    let
      kb = bytes / 1024.0;
      mb = bytes / (1024.0 * 1024.0);
      # Format with one decimal place
      formatDecimal = n:
        let
          whole = builtins.ceil n;
          decimal = builtins.ceil ((n - builtins.floor n) * 10);
        in
        "${toString whole}.${toString decimal}";
    in
    if bytes < 1024 then "${toString bytes} B"
    else if bytes < 1024 * 1024 then "${formatDecimal kb} KB"
    else "${formatDecimal mb} MB";

  # Group assets by platform
  groupAssetsByPlatform = assets:
    let
      filtered = lib.filter
        (a: !lib.hasSuffix ".sha256" a.name && !lib.hasSuffix ".sig" a.name && !lib.hasSuffix ".txt" a.name)
        assets;
      grouped = lib.groupBy (a: getPlatform a.name) filtered;
    in
    grouped;

  # Render platform download list HTML
  renderPlatformList = assets:
    let
      grouped = groupAssetsByPlatform assets;
      platforms = [ "linux-amd64" "linux-arm64" "darwin-amd64" "darwin-arm64" "windows-amd64" "other" ];
      validPlatforms = lib.filter (p: grouped ? ${p} && grouped.${p} != [ ]) platforms;
    in
    if validPlatforms == [ ] then
      "<li>No downloads available</li>"
    else
      lib.concatMapStringsSep "\n"
        (platform:
          let
            platformAssets = grouped.${platform};
            primary = lib.head platformAssets;
            size = formatSize primary.size;
          in
          ''
            <li>
              <span class="platform-name">${html.escape platformNames.${platform}}</span>
              <span class="platform-link">
                <a href="${html.escape primary.browser_download_url}" class="download-link">${html.escape primary.name}</a>
                <span class="file-size">(${size})</span>
              </span>
            </li>
          ''
        )
        validPlatforms;

  # Get latest non-draft release
  latestRelease =
    let nonDraft = lib.filter (r: !(r.draft or false)) releases;
    in if nonDraft == [ ] then null else lib.head nonDraft;

  # Generate latest release HTML
  latestReleaseHtml =
    if latestRelease == null then
      ''<p>No releases available yet. Visit <a href="https://github.com/${githubRepo}/releases">GitHub Releases</a>.</p>''
    else
      let
        prereleaseBadge = if latestRelease.prerelease or false then '' <span class="prerelease-badge">Pre-release</span>'' else "";
        date = lib.substring 0 10 latestRelease.published_at; # Extract YYYY-MM-DD
      in
      ''
        <div class="release-version latest">
          <div class="release-header">
            <h3>${html.escape latestRelease.tag_name}${prereleaseBadge} <span class="latest-badge">Latest</span></h3>
            <span class="release-date">${date}</span>
          </div>
          <ul class="platform-list">
            ${renderPlatformList (latestRelease.assets or [])}
          </ul>
        </div>
      '';

  # Generate all releases HTML for downloads page
  allReleasesHtml =
    let
      nonDraft = lib.filter (r: !(r.draft or false)) releases;
      # Take first 5 releases
      recent = if nonDraft == [ ] then [ ] else lib.take 5 nonDraft;
      latestId = if nonDraft == [ ] then null else (lib.head nonDraft).id;
    in
    if recent == [ ] then
      ''<p>No releases available yet. Visit <a href="https://github.com/${githubRepo}/releases">GitHub Releases</a>.</p>''
    else
      lib.concatMapStringsSep "\n"
        (release:
          let
            isLatest = latestId != null && release.id == latestId;
            prereleaseBadge = if release.prerelease or false then '' <span class="prerelease-badge">Pre-release</span>'' else "";
            latestBadge = if isLatest then '' <span class="latest-badge">Latest</span>'' else "";
            date = lib.substring 0 10 release.published_at;
          in
          ''
            <div class="release-version ${if isLatest then "latest" else ""}">
              <div class="release-header">
                <h3>${html.escape release.tag_name}${prereleaseBadge}${latestBadge}</h3>
                <span class="release-date">${date}</span>
              </div>
              <ul class="platform-list">
                ${renderPlatformList (release.assets or [])}
              </ul>
            </div>
          ''
        )
        recent;

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
    prism = pkgs.fetchurl {
      url = "https://cdn.jsdelivr.net/npm/prismjs@1.29.0/themes/prism-tomorrow.min.css";
      hash = "sha256-GxX+KXGZigSK67YPJvbu12EiBx257zuZWr0AMiT1Kpg=";
    };
  };

  jsFiles = {
    prismCore = pkgs.fetchurl {
      url = "https://cdn.jsdelivr.net/npm/prismjs@1.29.0/components/prism-core.min.js";
      hash = "sha256-4mJNT2bMXxcc1GCJaxBmMPdmah5ji0Ldnd79DKd1hoM=";
    };
    prismAutoloader = pkgs.fetchurl {
      url = "https://cdn.jsdelivr.net/npm/prismjs@1.29.0/plugins/autoloader/prism-autoloader.min.js";
      hash = "sha256-AjM0J5XIbiB590BrznLEgZGLnOQWrt62s3BEq65Q/I0=";
    };
  };

  logo = ./web/qntx.jpg;

  # Default OpenGraph description
  defaultDescription = "Continuous Intelligence - systems that continuously evolve their understanding through verifiable attestations.";

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

  # Generate BreadcrumbList JSON-LD from fileInfo
  mkBreadcrumbJsonLd = fileInfo:
    let
      category = if fileInfo.dir == "." then null else lib.head (lib.splitString "/" fileInfo.dir);
      categoryTitle = if category == null then null else toTitleCase category;
      documentTitle = toTitleCase fileInfo.name;
      canonicalUrl = "${baseUrl}/${fileInfo.htmlPath}";

      homeItem = ''
        {
          "@type": "ListItem",
          "position": 1,
          "name": "Home",
          "item": "${baseUrl}/"
        }'';

      categoryItem = lib.optionalString (category != null) '',
        {
          "@type": "ListItem",
          "position": 2,
          "name": "${html.escapeJson categoryTitle}",
          "item": "${baseUrl}/${category}/"
        }'';

      # Last item doesn't need "item" URL (it's the current page)
      docPosition = if category == null then 2 else 3;
      docItem = '',
        {
          "@type": "ListItem",
          "position": ${toString docPosition},
          "name": "${html.escapeJson documentTitle}"
        }'';
    in
    ''
      <!-- BreadcrumbList JSON-LD -->
      <script type="application/ld+json">
      {
        "@context": "https://schema.org",
        "@type": "BreadcrumbList",
        "itemListElement": [${homeItem}${categoryItem}${docItem}
        ]
      }
      </script>'';

  mkHead = { title, prefix, description ? defaultDescription, pagePath ? "", breadcrumbJsonLd ? "", additionalJsonLd ? "" }:
    let
      canonicalUrl = "${baseUrl}${if pagePath == "" then "/" else "/${pagePath}"}";
    in ''
    <!DOCTYPE html>
    <html lang="en">
    <head>
      <meta charset="UTF-8">
      <meta name="viewport" content="width=device-width, initial-scale=1.0">
      <title>${html.escape title}</title>
      <meta name="description" content="${html.escape description}">
      <link rel="icon" type="image/jpeg" href="${prefix}/qntx.jpg">
      <link rel="canonical" href="${canonicalUrl}">
      <link rel="stylesheet" href="${prefix}/css/core.css">
      <link rel="stylesheet" href="${prefix}/css/docs.css">
      <link rel="stylesheet" href="${prefix}/css/prism.css">
      <!-- OpenGraph -->
      <meta property="og:title" content="${html.escape title}">
      <meta property="og:description" content="${html.escape description}">
      <meta property="og:image" content="${baseUrl}/qntx.jpg">
      <meta property="og:url" content="${canonicalUrl}">
      <meta property="og:type" content="website">
      <meta property="og:site_name" content="QNTX">
      <!-- Twitter Card -->
      <meta name="twitter:card" content="summary">
      <meta name="twitter:title" content="${html.escape title}">
      <meta name="twitter:description" content="${html.escape description}">
      <meta name="twitter:image" content="${baseUrl}/qntx.jpg">
      <!-- RSS Feed -->
      <link rel="alternate" type="application/rss+xml" title="QNTX Documentation" href="${baseUrl}/feed.xml">
      <!-- JSON-LD Structured Data -->
      <script type="application/ld+json">
      {
        "@context": "https://schema.org",
        "@type": "TechArticle",
        "headline": "${html.escapeJson title}",
        "description": "${html.escapeJson description}",
        "url": "${canonicalUrl}",
        "image": "${baseUrl}/qntx.jpg",
        ${lib.optionalString (provenance.date != null) ''"datePublished": "${provenance.date}",
        "dateModified": "${provenance.date}",''}
        "publisher": {
          "@type": "Organization",
          "name": "QNTX",
          "logo": {
            "@type": "ImageObject",
            "url": "${baseUrl}/qntx.jpg"
          }
        }
      }
      </script>
      ${breadcrumbJsonLd}
      ${additionalJsonLd}
    </head>'';

  mkNav = { prefix }: ''
    <nav class="doc-nav">
      <a href="${prefix}/index.html">
        <img src="${prefix}/qntx.jpg" alt="QNTX" class="site-logo">Documentation Home
      </a>
    </nav>'';

  mkBreadcrumb = fileInfo:
    let
      category = if fileInfo.dir == "." then null else lib.head (lib.splitString "/" fileInfo.dir);
      categoryMeta = if category == null then null else getCategoryMeta category;
      categoryTitle = if category == null then null else toTitleCase category;
      categorySymbol = if categoryMeta == null then "" else categoryMeta.symbol;
      documentTitle = toTitleCase fileInfo.name;

      homeCrumb = ''<a href="${fileInfo.prefix}/index.html">Home</a>'';
      categoryCrumb = if category == null then "" else
      ''<span class="breadcrumb-sep">›</span><span class="breadcrumb-category">${categorySymbol} ${categoryTitle}</span>'';
      documentCrumb = ''<span class="breadcrumb-sep">›</span><span class="breadcrumb-current">${documentTitle}</span>'';
    in
    ''<nav class="breadcrumb">${homeCrumb}${categoryCrumb}${documentCrumb}</nav>'';

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
    , description ? defaultDescription
    , pagePath ? ""
    , additionalJsonLd ? ""
    }:
    let
      scriptTags = lib.concatMapStringsSep "\n"
        (s: ''<script src="${prefix}/js/${s}"></script>'')
        scripts;
    in
    ''
      ${mkHead { inherit title prefix description pagePath additionalJsonLd; }}
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
      descHtml = lib.optionalString (meta.desc != "") ''<span class="category-desc">${html.escape meta.desc}</span>'';
      fileLinks = lib.concatMapStringsSep "\n"
        (f: ''<li><a href="${f.htmlPath}"><span class="doc-name">${html.escape (toTitleCase f.name)}</span></a></li>'')
        sortedFiles;
    in
    ''
      <section class="category-section">
        <div class="category-header">
          <h2 class="category-title">${html.escape categoryTitle}</h2>${descHtml}
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
      categorySections = lib.concatMapStringsSep "\n"
        (cat: genIndexSection cat groupedFiles.${cat})
        allCats;

      introText = ''
        <div class="intro-section">
          <h2 style="text-align: center; margin: 20px 0;">Continuous Intelligence</h2>
          <p>QNTX implements <strong>Continuous Intelligence</strong> - systems that continuously evolve their understanding through verifiable attestations.</p>
          <ul>
            <li><strong>⨳ ix</strong> - Ingest data from plugins, APIs, and actions into attestations</li>
            <li><strong>꩜ Pulse</strong> - Background jobs that enrich and connect knowledge</li>
            <li><strong>⋈ ax</strong> - Ask questions across time, trace causality, explore relationships</li>
          </ul>
        </div>
      '';

      buildPipelineInfo = ''
        <section class="build-info-section">
          <h2>Build & CI Pipeline</h2>
          <p>QNTX uses <strong>Nix</strong> for fully reproducible builds. Every commit triggers automated builds, tests, and container image publishing via GitHub Actions.</p>
          <ul class="build-info-list">
            <li><strong>Build System:</strong> Nix flakes with locked dependencies</li>
            <li><strong>CI/CD:</strong> GitHub Actions with Cachix binary cache</li>
            <li><strong>Artifacts:</strong> Linux/macOS/Windows binaries, OCI containers, documentation</li>
            <li><strong>Testing:</strong> Go tests, type generation validation, integration tests</li>
          </ul>
          <p><a href="./infrastructure.html">View full infrastructure documentation</a></p>
        </section>
      '';
    in
    mkPage {
      title = "QNTX - Continuous Intelligence";
      nav = false;
      scripts = [ ];
      pagePath = "index.html";
      content = ''
        <div class="doc-header">
          <img src="./qntx.jpg" alt="QNTX Logo">
          <h1>QNTX</h1>
          <p style="margin-top: 0; font-size: 1.1em;">Continuous Intelligence</p>
        </div>

        <nav class="quick-links">
          ${quickLinksHtml}
        </nav>

        ${introText}

        <section class="download-section quick-download">
          <h2>Quick Download</h2>
          ${latestReleaseHtml}
          <p style="margin-top: 8px; font-size: 0.9em;">
            <a href="./downloads.html">View all downloads and installation options</a>
          </p>
        </section>

        ${buildPipelineInfo}

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
        content = allReleasesHtml;
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

      # SoftwareApplication JSON-LD for downloads page
      softwareAppJsonLd =
        let
          version = if latestRelease != null then latestRelease.tag_name else "latest";
          releaseDate = if latestRelease != null then lib.substring 0 10 latestRelease.published_at else null;
        in
        ''
        <!-- SoftwareApplication JSON-LD -->
        <script type="application/ld+json">
        {
          "@context": "https://schema.org",
          "@type": "SoftwareApplication",
          "name": "QNTX",
          "description": "Continuous Intelligence - systems that continuously evolve their understanding through verifiable attestations.",
          "applicationCategory": "DeveloperApplication",
          "operatingSystem": "Linux, macOS, Windows",
          "softwareVersion": "${html.escapeJson version}",
          ${lib.optionalString (releaseDate != null) ''"datePublished": "${releaseDate}",''}
          "downloadUrl": "https://github.com/${githubRepo}/releases",
          "installUrl": "https://github.com/${githubRepo}#installation",
          "releaseNotes": "https://github.com/${githubRepo}/releases",
          "license": "https://github.com/${githubRepo}/blob/main/LICENSE",
          "author": {
            "@type": "Organization",
            "name": "QNTX",
            "url": "${baseUrl}"
          },
          "offers": {
            "@type": "Offer",
            "price": "0",
            "priceCurrency": "USD"
          }
        }
        </script>'';
    in
    mkPage {
      title = "QNTX Downloads";
      description = "Download QNTX binaries, Docker images, or install via Nix package manager.";
      pagePath = "downloads.html";
      scripts = [ ];
      additionalJsonLd = softwareAppJsonLd;
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
      description = "QNTX build infrastructure - Nix packages, apps, containers, and reproducible builds.";
      pagePath = "infrastructure.html";
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
        { path = "feed.xml"; desc = "RSS feed"; }
        { path = "sitemap.xml"; desc = "XML sitemap"; }
        { path = "sitemap.xsl"; desc = "Sitemap stylesheet"; }
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
            [ "<strong>GitHub Releases</strong>" "Static release downloads fetched at build time via Nix fixed-output derivation" ]
            [ "<strong>Provenance</strong>" "Every page shows commit, tag, date, CI user, and pipeline info" ]
            [ "<strong>Self-documenting Infra</strong>" "Nix packages, apps, and containers documented from flake metadata" ]
            [ "<strong>Incremental Builds</strong>" "Each markdown file is a separate derivation for faster rebuilds" ]
            [ "<strong>OpenGraph &amp; Twitter Cards</strong>" "Social media preview cards with per-page titles, descriptions, and images" ]
            [ "<strong>RSS Feed</strong>" "Subscribe to documentation updates via <code>/feed.xml</code> with autodiscovery" ]
            [ "<strong>Sitemap with XSLT</strong>" "Human-readable sitemap at <code>/sitemap.xml</code> with browser-viewable styling" ]
            [ "<strong>Canonical URLs</strong>" "Every page has a canonical URL for proper SEO indexing" ]
            [ "<strong>JSON-LD</strong>" "TechArticle, BreadcrumbList, and SoftwareApplication schemas for rich search snippets" ]
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
      description = "Documentation about the QNTX static site generator written in pure Nix.";
      pagePath = "sitegen.html";
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
      # Generate description from category
      category = if fileInfo.dir == "." then null else lib.head (lib.splitString "/" fileInfo.dir);
      catMeta = if category == null then null else getCategoryMeta category;
      docDescription =
        if catMeta != null && catMeta.desc != ""
        then "QNTX ${toTitleCase category} - ${catMeta.desc}"
        else "QNTX documentation - ${toTitleCase fileInfo.name}";
      breadcrumbJsonLd = mkBreadcrumbJsonLd fileInfo;
    in
    pkgs.runCommand "qntx-doc-${fileInfo.name}"
      {
        nativeBuildInputs = [ pkgs.pulldown-cmark ];
      }
      ''
        mkdir -p "$out/$(dirname "${fileInfo.htmlPath}")"
        {
          cat <<'EOF'
        ${mkHead { title = "QNTX - ${fileInfo.name}"; prefix = fileInfo.prefix; description = docDescription; pagePath = fileInfo.htmlPath; inherit breadcrumbJsonLd; }}
        <body>
        ${mkNav { prefix = fileInfo.prefix; }}
        ${mkBreadcrumb fileInfo}
        EOF
          cat <<'EOF' | ${pkgs.pulldown-cmark}/bin/pulldown-cmark -T -S -F
        ${rewrittenMd}
        EOF
          cat <<'EOF'
        ${provenanceFooter}
        <script src="${fileInfo.prefix}/js/prismCore.js"></script>
        <script src="${fileInfo.prefix}/js/prismAutoloader.js"></script>
        </body>
        </html>
        EOF
        } > "$out/${fileInfo.htmlPath}"
      '';

  htmlDerivations = map mkHtmlDerivation fileInfos;

  # ============================================================================
  # Sitemap Generation
  # ============================================================================

  # Derive base URL from CNAME content (single source of truth)
  cnameContent = "qntx.sbvh.nl";
  baseUrl = "https://${cnameContent}";

  # Convert Unix timestamp to YYYY-MM-DD
  timestampToDate = ts:
    let
      days = ts / 86400;
      # Years since 1970 (accounting for leap years approximately)
      year = 1970 + (days / 365);
      remainingDays = lib.mod days 365;
      month = 1 + (remainingDays / 30);
      day = 1 + (lib.mod remainingDays 30);
      pad = n: if n < 10 then "0${toString n}" else toString n;
    in
    "${toString year}-${pad month}-${pad day}";

  # Fallback lastmod: buildDate (CI) or gitCommitDate converted to YYYY-MM-DD (local builds)
  sitemapLastmod =
    if buildDate != null then buildDate
    else if gitCommitDate != null then timestampToDate gitCommitDate
    else null;

  # Generate sitemap entries for all HTML pages
  mkSitemapUrl = { loc, lastmod ? sitemapLastmod, changefreq ? "weekly", priority ? "0.6" }:
    ''
      <url>
        <loc>${baseUrl}${loc}</loc>
        ${lib.optionalString (lastmod != null) "<lastmod>${lastmod}</lastmod>"}
        <changefreq>${changefreq}</changefreq>
        <priority>${priority}</priority>
      </url>'';

  sitemapUrls =
    # Index page (highest priority)
    [ (mkSitemapUrl { loc = "/"; priority = "1.0"; changefreq = "daily"; }) ]

    # Special pages
    ++ [
      (mkSitemapUrl { loc = "/downloads.html"; priority = "0.9"; changefreq = "daily"; })
      (mkSitemapUrl { loc = "/infrastructure.html"; priority = "0.7"; })
      (mkSitemapUrl { loc = "/sitegen.html"; priority = "0.7"; })
    ]

    # All documentation pages
    ++ map
      (fileInfo: mkSitemapUrl {
        loc = "/${fileInfo.htmlPath}";
        priority = if fileInfo.depth == 0 then "0.8" else "0.6";
      })
      fileInfos;

  sitemapContent = ''
    <?xml version="1.0" encoding="UTF-8"?>
    <?xml-stylesheet type="text/xsl" href="sitemap.xsl"?>
    <urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
    ${lib.concatStringsSep "\n" sitemapUrls}
    </urlset>
  '';

  # XSLT stylesheet for human-readable sitemap viewing in browsers
  sitemapXslContent = ''
    <?xml version="1.0" encoding="UTF-8"?>
    <xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform" xmlns:sitemap="http://www.sitemaps.org/schemas/sitemap/0.9">
      <xsl:output method="html" encoding="UTF-8"/>
      <xsl:template match="/">
        <html>
          <head>
            <title>QNTX Sitemap</title>
            <style>
              body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; margin: 40px auto; max-width: 900px; padding: 0 20px; background: #1e1e1e; color: #e0e0e0; }
              h1 { color: #fff; border-bottom: 1px solid #333; padding-bottom: 10px; }
              p { color: #aaa; margin-bottom: 20px; }
              table { width: 100%; border-collapse: collapse; }
              th { text-align: left; padding: 12px; background: #252525; color: #fff; border-bottom: 2px solid #333; }
              td { padding: 10px 12px; border-bottom: 1px solid #333; }
              tr:hover td { background: #2a2a2a; }
              a { color: #66b3ff; text-decoration: none; }
              a:hover { text-decoration: underline; }
              .priority { color: #888; }
              .changefreq { color: #888; }
              .lastmod { color: #888; }
            </style>
          </head>
          <body>
            <h1>QNTX Sitemap</h1>
            <p>This sitemap contains <xsl:value-of select="count(sitemap:urlset/sitemap:url)"/> URLs.</p>
            <table>
              <tr>
                <th>URL</th>
                <th>Priority</th>
                <th>Change Freq</th>
                <th>Last Modified</th>
              </tr>
              <xsl:for-each select="sitemap:urlset/sitemap:url">
                <xsl:sort select="sitemap:priority" order="descending"/>
                <tr>
                  <td><a href="{sitemap:loc}"><xsl:value-of select="sitemap:loc"/></a></td>
                  <td class="priority"><xsl:value-of select="sitemap:priority"/></td>
                  <td class="changefreq"><xsl:value-of select="sitemap:changefreq"/></td>
                  <td class="lastmod"><xsl:value-of select="sitemap:lastmod"/></td>
                </tr>
              </xsl:for-each>
            </table>
          </body>
        </html>
      </xsl:template>
    </xsl:stylesheet>
  '';

  # ============================================================================
  # RSS Feed Generation
  # ============================================================================

  # RSS date format: RFC 822 (e.g., "Mon, 15 Jan 2025 00:00:00 GMT")
  # We approximate since we only have YYYY-MM-DD from sitemapLastmod
  rssDate =
    if sitemapLastmod != null
    then "${sitemapLastmod}T00:00:00Z"
    else null;

  # Generate RSS item for a documentation page
  mkRssItem = { title, link, description, category ? null }:
    ''
      <item>
        <title>${html.escape title}</title>
        <link>${baseUrl}${link}</link>
        <guid isPermaLink="true">${baseUrl}${link}</guid>
        <description>${html.escape description}</description>
        ${lib.optionalString (category != null) "<category>${html.escape category}</category>"}
        ${lib.optionalString (rssDate != null) "<pubDate>${rssDate}</pubDate>"}
      </item>'';

  # Generate RSS items for documentation pages
  rssItems =
    # Special pages
    [
      (mkRssItem {
        title = "QNTX Downloads";
        link = "/downloads.html";
        description = "Download QNTX binaries, Docker images, or install via Nix package manager.";
      })
      (mkRssItem {
        title = "QNTX Infrastructure";
        link = "/infrastructure.html";
        description = "QNTX build infrastructure - Nix packages, apps, containers, and reproducible builds.";
      })
    ]
    # All documentation pages
    ++ map
      (fileInfo:
        let
          category = if fileInfo.dir == "." then null else lib.head (lib.splitString "/" fileInfo.dir);
          catMeta = if category == null then null else getCategoryMeta category;
          itemDescription =
            if catMeta != null && catMeta.desc != ""
            then "QNTX ${toTitleCase category} - ${catMeta.desc}"
            else "QNTX documentation - ${toTitleCase fileInfo.name}";
        in
        mkRssItem {
          title = "QNTX - ${toTitleCase fileInfo.name}";
          link = "/${fileInfo.htmlPath}";
          description = itemDescription;
          category = if category != null then toTitleCase category else null;
        })
      fileInfos;

  rssFeedContent = ''
    <?xml version="1.0" encoding="UTF-8"?>
    <rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
      <channel>
        <title>QNTX Documentation</title>
        <link>${baseUrl}</link>
        <description>Continuous Intelligence - documentation and updates for QNTX</description>
        <language>en-us</language>
        <atom:link href="${baseUrl}/feed.xml" rel="self" type="application/rss+xml"/>
        ${lib.optionalString (rssDate != null) "<lastBuildDate>${rssDate}</lastBuildDate>"}
        <generator>sitegen.nix</generator>
        ${lib.concatStringsSep "\n    " rssItems}
      </channel>
    </rss>
  '';

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

    "CNAME" = pkgs.writeTextFile {
      name = "qntx-docs-cname";
      text = "${cnameContent}\n";
      destination = "/CNAME";
    };

    "sitemap.xml" = pkgs.writeTextFile {
      name = "qntx-docs-sitemap";
      text = sitemapContent;
      destination = "/sitemap.xml";
    };

    "sitemap.xsl" = pkgs.writeTextFile {
      name = "qntx-docs-sitemap-xsl";
      text = sitemapXslContent;
      destination = "/sitemap.xsl";
    };

    "feed.xml" = pkgs.writeTextFile {
      name = "qntx-docs-rss";
      text = rssFeedContent;
      destination = "/feed.xml";
    };

    "robots.txt" = pkgs.writeTextFile {
      name = "qntx-docs-robots";
      text = ''
        User-agent: *
        Allow: /

        Sitemap: ${baseUrl}/sitemap.xml
      '';
      destination = "/robots.txt";
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
