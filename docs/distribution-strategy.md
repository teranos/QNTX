# QNTX Distribution Strategy

## Current State

### What We Build
- ✅ Docker images (amd64, arm64) → GHCR
- ✅ Go CLI binary (local builds only)
- ⚠️  Tauri apps (macOS, Windows, Android, iOS) → Only CI checks, no releases

### What We Distribute
- Docker images: `ghcr.io/teranos/qntx:latest` and `ghcr.io/teranos/qntx:{version}`
- **Nothing else publicly available**

## Distribution Channels

### 1. GitHub Releases (Quick Win - High Priority)

**What:** Attach binaries to git tags automatically

**Platforms to distribute:**
- CLI binaries (Linux amd64/arm64, macOS Intel/ARM, Windows x64)
- Desktop apps (macOS .dmg, Windows .msi/.exe)
- Android APK (sideload)

**Implementation:**
- Add release workflow triggered on `v*` tags
- Use `goreleaser` for CLI multi-platform builds
- Use Tauri's built-in release action for desktop apps
- Upload artifacts to GitHub Release

**Benefits:**
- Immediate download links for all platforms
- Changelog integration
- Version history
- Zero infrastructure cost

**Effort:** Medium (1-2 days)

---

### 2. Homebrew (High Priority - macOS/Linux)

**What:** Package manager for macOS and Linux

**Command:**
```bash
brew install teranos/tap/qntx
```

**Requirements:**
- Create homebrew-tap repository (`teranos/homebrew-tap`)
- Formula pointing to GitHub Release binaries
- Auto-update formula on new releases

**Benefits:**
- Primary distribution method for developers
- Automatic updates via `brew upgrade`
- High discoverability

**Effort:** Low (few hours after GitHub Releases exist)

---

### 3. Container Registries (Already Done ✓)

**Current:**
- ✅ GitHub Container Registry (ghcr.io)

**Could Add:**
- Docker Hub (wider discoverability)
- Quay.io (Red Hat ecosystem)

**Multi-arch manifests:** ✅ Already implemented

---

### 4. Package Managers by Platform

#### macOS
- ✅ Homebrew (priority)
- MacPorts (lower priority)
- Mac App Store (requires Apple Developer account + annual fee)

#### Windows
- **winget** (High priority - official Windows package manager)
  - Submit manifest to `microsoft/winget-pkgs`
  - Free, official, widely adopted
- **Scoop** (Medium priority - developer-focused)
  - Add bucket with JSON manifest
- **Chocolatey** (Lower priority - enterprise-focused)
  - Requires moderation for community repo

#### Linux
- **APT repository** (Debian/Ubuntu)
  - Host .deb packages on GitHub Pages or Cloudflare Pages
  - One-time setup, auto-update via CI
- **RPM repository** (Fedora/RHEL)
  - Similar to APT
- **Snap Store** (Cross-distro)
  - Canonical's universal package format
  - Automatic updates, sandboxed
- **Flatpak** (Desktop apps)
  - For Tauri desktop app
  - Flathub distribution

---

### 5. Mobile App Stores

#### Android
**Options:**
1. **Google Play Store** (Recommended)
   - Widest reach
   - Requires $25 one-time fee
   - Review process (1-3 days typically)

2. **F-Droid** (Open source alternative)
   - Free, no developer account needed
   - Longer review process
   - Trusted by privacy-conscious users

3. **GitHub Releases** (Sideloading)
   - Immediate, free
   - Lower discoverability
   - Users must enable "unknown sources"

**Recommendation:** Start with GitHub Releases (APK), add Play Store when ready for wider distribution.

#### iOS
- **App Store** (Only official option)
  - Requires Apple Developer account ($99/year)
  - Strict review process
  - TestFlight for beta distribution

---

### 6. Language-Specific Package Managers

**Not Recommended** (QNTX is a platform, not a library):
- ❌ npm - Not applicable (QNTX is not a Node library)
- ❌ PyPI - Python plugin exists but QNTX itself isn't a Python package
- ❌ crates.io - Not distributing Rust library

---

## Implementation Roadmap

### Phase 1: Foundation (Week 1)
**Goal:** Make QNTX downloadable

1. **GitHub Releases workflow**
   - Multi-platform CLI builds (goreleaser)
   - Tauri desktop app builds (macOS .dmg, Windows .msi)
   - Android APK build
   - Auto-create release on version tags

2. **Update README**
   - Installation instructions
   - Download links
   - Platform support matrix

**Deliverable:** Users can download QNTX for their platform from GitHub

---

### Phase 2: Package Managers (Week 2-3)
**Goal:** Native installation experience

1. **Homebrew tap**
   - Create `teranos/homebrew-tap`
   - Auto-update formula on releases
   - Test on macOS and Linux

2. **winget manifest**
   - Submit to microsoft/winget-pkgs
   - Waiting period for approval (~1 week)

**Deliverable:**
```bash
brew install teranos/tap/qntx        # macOS/Linux
winget install Teranos.QNTX          # Windows
```

---

### Phase 3: Linux Distribution (Week 4)
**Goal:** Native package managers for Linux

1. **APT repository** (Debian/Ubuntu)
   - Host on GitHub Pages
   - Auto-publish .deb on releases
   - Add to sources.list

2. **Snap package**
   - Create snapcraft.yaml
   - Publish to Snap Store
   - Automatic updates

**Deliverable:**
```bash
sudo apt install qntx               # Debian/Ubuntu
snap install qntx                   # Any Linux
```

---

### Phase 4: Mobile (Month 2)
**Goal:** Official app store presence

1. **Google Play Store**
   - Developer account setup
   - App listing (screenshots, description)
   - Submit Android APK/AAB
   - Production release

2. **iOS App Store** (if budget allows)
   - Apple Developer account ($99/year)
   - App listing
   - TestFlight beta
   - Production release

---

## Technical Implementation Details

### CLI Binary Distribution

**Using goreleaser:**

`.goreleaser.yml`:
```yaml
builds:
  - main: ./cmd/qntx
    binary: qntx
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -X github.com/teranos/QNTX/internal/version.Version={{.Version}}
      - -X github.com/teranos/QNTX/internal/version.CommitHash={{.Commit}}

archives:
  - format: tar.gz
    format_overrides:
      - goos: windows
        format: zip

release:
  github:
    owner: teranos
    name: QNTX
```

**GitHub workflow:**
```yaml
- uses: goreleaser/goreleaser-action@v5
  with:
    version: latest
    args: release --clean
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

---

### Tauri App Distribution

**Add to existing workflows:**

```yaml
- name: Build Tauri app
  uses: tauri-apps/tauri-action@v0
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  with:
    tagName: ${{ github.ref_name }}
    releaseName: 'QNTX ${{ github.ref_name }}'
    releaseBody: 'See CHANGELOG.md for details'
    releaseDraft: false
    prerelease: false
```

Tauri automatically:
- Builds platform-specific installers
- Uploads to GitHub Releases
- Generates update manifests (for auto-update)

---

### Homebrew Formula

**`homebrew-tap/Formula/qntx.rb`:**
```ruby
class Qntx < Formula
  desc "Continuous Intelligence Platform"
  homepage "https://github.com/teranos/QNTX"
  version "0.16.14"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/teranos/QNTX/releases/download/v0.16.14/qntx_Darwin_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/teranos/QNTX/releases/download/v0.16.14/qntx_Darwin_x86_64.tar.gz"
      sha256 "..."
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/teranos/QNTX/releases/download/v0.16.14/qntx_Linux_arm64.tar.gz"
      sha256 "..."
    else
      url "https://github.com/teranos/QNTX/releases/download/v0.16.14/qntx_Linux_x86_64.tar.gz"
      sha256 "..."
    end
  end

  def install
    bin.install "qntx"
  end
end
```

Auto-update with `brew bump-formula-pr`.

---

### winget Manifest

**`microsoft/winget-pkgs/manifests/t/Teranos/QNTX/0.16.14/`:**

```yaml
PackageIdentifier: Teranos.QNTX
PackageVersion: 0.16.14
PackageLocale: en-US
Publisher: Teranos
PackageName: QNTX
License: MIT
ShortDescription: Continuous Intelligence Platform
Installers:
  - Architecture: x64
    InstallerType: msi
    InstallerUrl: https://github.com/teranos/QNTX/releases/download/v0.16.14/qntx_Windows_x64.msi
    InstallerSha256: ...
```

---

## Cost Analysis

| Channel | Setup Cost | Ongoing Cost | Effort |
|---------|-----------|--------------|--------|
| GitHub Releases | Free | Free | Medium |
| Homebrew | Free | Free | Low |
| winget | Free | Free | Low |
| Docker (GHCR) | Free | Free | Done ✓ |
| Snap Store | Free | Free | Medium |
| Google Play | $25 once | Free | Medium |
| Apple App Store | Free (have account?) | $99/year | High |
| F-Droid | Free | Free | Medium |

**Total upfront:** $25-124 (depending on iOS)
**Annual:** $0-99 (depending on iOS)

---

## Metrics to Track

Post-distribution, monitor:
- Download counts by platform (GitHub Releases API)
- Package manager installs (Homebrew analytics, winget telemetry)
- Docker pulls (GHCR stats)
- App store downloads/ratings
- Platform distribution (which OS/arch is most popular)

---

## Recommended Priority

1. **GitHub Releases** (CLI + desktop apps) - Week 1
2. **Homebrew** - Week 2
3. **winget** - Week 2
4. **Docker** - Already done ✓
5. **Snap/APT** - Week 3-4
6. **Google Play** - Month 2
7. **iOS App Store** - When budget allows

---

## Next Steps

1. Create `.goreleaser.yml` for CLI builds
2. Add release workflow (`.github/workflows/release.yml`)
3. Update Tauri workflows to build on tags (not just check)
4. Create release (tag v0.16.15 to test)
5. Document installation methods in README
6. Create homebrew-tap repository
7. Submit winget manifest

---

## Questions to Answer

- Do we want auto-update for desktop apps? (Tauri supports this)
- Should Docker images be pushed to Docker Hub in addition to GHCR?
- Do we want nightly/beta releases in addition to stable?
- What's our iOS distribution timeline? (requires Apple Developer account)
- Should we create a downloads page on a website? (or just README)
