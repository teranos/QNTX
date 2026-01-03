/**
 * QNTX Web Bundle Builder
 *
 * Bundles all web assets into internal/server/dist/ for embedding in Go binary
 *
 * This is sacred infrastructure code. Every line has blast radius across:
 * - Go embedding (go:embed path)
 * - Deployment (CloudFront/S3, Vercel)
 * - Developer workflow (build step in Makefile)
 * - Binary size and structure
 */

import { cp, mkdir, readdir, rm } from "fs/promises";
import { join } from "path";

const sourceDir = import.meta.dir; // web/
const outputDir = join(sourceDir, "..", "internal", "server", "dist");

// Peach color palette
const peach = "\x1b[38;5;217m";
const darkPeach = "\x1b[38;5;216m";
const lightPeach = "\x1b[38;5;223m";
const reset = "\x1b[0m";
const dim = "\x1b[2m";

console.log(`${peach}Building QNTX Web UI...${reset}`);
console.log(`${dim}   Source: ${sourceDir}${reset}`);
console.log(`${dim}   Output: ${outputDir}${reset}`);

try {
  // Clean output directory
  console.log(`${darkPeach}Cleaning output directory...${reset}`);
  try {
    await rm(outputDir, { recursive: true, force: true });
  } catch (e) {
    // Directory might not exist yet
  }

  // Create output directory
  await mkdir(outputDir, { recursive: true });

  // Bundle TypeScript with Bun
  console.log(`${darkPeach}Bundling JavaScript...${reset}`);
  await Bun.build({
    entrypoints: [join(sourceDir, "ts", "main.ts")],
    outdir: join(outputDir, "js"),
    minify: true,
    sourcemap: "none",
    splitting: false, // Disable code splitting to ensure single bundle
    // Define aliases to force single instance of CodeMirror modules
    external: [], // Bundle everything, don't externalize anything
  });

  // Copy CSS first (needed to scan for files)
  console.log(`${darkPeach}Copying CSS...${reset}`);
  await cp(join(sourceDir, "css"), join(outputDir, "css"), { recursive: true });

  // Scan CSS directory to auto-generate link tags
  const cssFiles = await readdir(join(sourceDir, "css"));
  const cssLinkTags = cssFiles
    .filter(file => file.endsWith('.css'))
    .sort() // Alphabetical order for consistency
    .map(file => `    <link rel="stylesheet" href="/css/${file}">`)
    .join('\n');

  // Copy HTML and update script reference
  console.log(`${darkPeach}Copying HTML...${reset}`);
  const htmlContent = await Bun.file(join(sourceDir, "index.html")).text();

  // Update the script src from /ts/main.ts to /js/main.js (the bundled output)
  let updatedHtml = htmlContent.replace('/ts/main.ts', '/js/main.js');

  // Replace CSS link tags with auto-generated ones
  // Find the section between <!-- CSS START --> and <!-- CSS END --> markers
  const cssMarkerStart = '<!-- CSS AUTO-GENERATED - DO NOT EDIT MANUALLY -->';
  const cssMarkerEnd = '<!-- END CSS AUTO-GENERATED -->';

  if (updatedHtml.includes(cssMarkerStart)) {
    // Replace existing auto-generated section
    const startIdx = updatedHtml.indexOf(cssMarkerStart);
    const endIdx = updatedHtml.indexOf(cssMarkerEnd);
    if (endIdx > startIdx) {
      updatedHtml = updatedHtml.substring(0, startIdx + cssMarkerStart.length) +
        '\n' + cssLinkTags + '\n    ' +
        updatedHtml.substring(endIdx);
    }
  }

  // Inject backend URL if provided via environment variable (for Vercel/static hosting)
  const backendUrl = process.env.BACKEND_URL;
  if (backendUrl) {
    console.log(`${lightPeach}Injecting backend URL: ${backendUrl}${reset}`);
    updatedHtml = updatedHtml.replace(
      "</head>",
      `<script>
        // Backend URL for WebSocket connections
        window.__BACKEND_URL__ = "${backendUrl}";
      </script>
      </head>`
    );
  }

  await Bun.write(join(outputDir, "index.html"), updatedHtml);

  // Copy fonts
  console.log(`${darkPeach}Copying fonts...${reset}`);
  await cp(join(sourceDir, "fonts"), join(outputDir, "fonts"), { recursive: true });

  // Copy vendor libraries (d3, pre-built bundles)
  console.log(`${darkPeach}Copying vendor libraries...${reset}`);
  await cp(join(sourceDir, "ts", "vendor"), join(outputDir, "js", "vendor"), { recursive: true });

  // Copy static assets
  console.log(`${darkPeach}Copying static assets...${reset}`);
  await cp(join(sourceDir, "qntx.jpg"), join(outputDir, "qntx.jpg"));

  console.log(`${peach}Build complete!${reset}`);
  console.log(`${dim}   Output ready at: ${outputDir}${reset}`);

} catch (error) {
  console.error(`${darkPeach}Build failed:${reset}`, error);
  process.exit(1);
}
