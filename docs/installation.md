# Installing QNTX

QNTX is available through multiple installation methods. Choose the one that best fits your workflow.

## Nix (Recommended)

QNTX uses Nix for reproducible builds and fast binary distribution via Cachix.

### Install

```bash
# Install QNTX CLI
nix profile install github:teranos/QNTX

# Verify installation
qntx --version
```

### Run Without Installing

```bash
# Run directly from GitHub
nix run github:teranos/QNTX -- --help

# Use in a temporary shell
nix shell github:teranos/QNTX
```

### Specific Version

```bash
# Install specific version (example)
nix profile install github:teranos/QNTX/v0.16.14

# Run specific version
nix run github:teranos/QNTX/v0.16.14

# List available versions
git ls-remote --tags https://github.com/teranos/QNTX.git
```

### Binary Cache

The first time you use QNTX via Nix, you'll see:

```
do you want to allow configuration setting 'extra-substituters' to be set to 'https://qntx.cachix.org' (y/N)?
```

**Accept this** (type `y`) for instant binary downloads instead of building from source.

The cache is configured in `flake.nix` and uses:
- Substituter: `https://qntx.cachix.org`
- Public key: `qntx.cachix.org-1:sL1EkSS5871D3ycLjHzuD+/zNddU9G38HGt3qQotAtg=`

---

## Docker

Multi-architecture images (amd64, arm64) are available on GitHub Container Registry.

### Pull and Run

```bash
# Latest version (auto-detects architecture)
docker pull ghcr.io/teranos/qntx:latest
docker run ghcr.io/teranos/qntx:latest qntx --help

# Specific version
docker pull ghcr.io/teranos/qntx:0.16.14
docker run ghcr.io/teranos/qntx:0.16.14 qntx --version
```

### Interactive Use

```bash
# Start container with shell
docker run -it ghcr.io/teranos/qntx:latest /bin/bash

# Inside container
qntx --help
```

### With Persistent Data

```bash
# Mount local directory for persistent storage
docker run -v $(pwd)/data:/data ghcr.io/teranos/qntx:latest qntx --help
```

---

## From Source

Build QNTX from source using Go and optionally Rust for fuzzy matching optimization.

### Prerequisites

- Go 1.24+ (required)
- Rust toolchain (optional, for fuzzy matching)
- Git

### Build

```bash
# Clone repository
git clone https://github.com/teranos/QNTX.git
cd QNTX

# Build with Rust fuzzy optimization (recommended)
make cli

# Or build without Rust (pure Go)
make cli-nocgo

# Binary created at ./bin/qntx
./bin/qntx --version
```

### Install Locally

```bash
# After building
sudo mv ./bin/qntx /usr/local/bin/

# Verify
qntx --version
```

---

## Package Managers (Coming Soon)

The following package managers are planned:

### Homebrew (macOS/Linux)
```bash
# Not yet available
brew install teranos/tap/qntx
```

### winget (Windows)
```bash
# Not yet available
winget install Teranos.QNTX
```

### APT (Debian/Ubuntu)
```bash
# Not yet available
sudo apt install qntx
```

---

## Verification

After installation, verify QNTX is working:

```bash
# Check version
qntx --version

# View help
qntx --help

# Check configuration
qntx am show
```

---

## Platform Support

| Platform | Architecture | Method | Status |
|----------|-------------|---------|---------|
| Linux | amd64 | Nix, Docker, Source | ✅ |
| Linux | arm64 | Nix, Docker, Source | ✅ |
| macOS | Intel (x64) | Nix, Source | ✅ |
| macOS | Apple Silicon (ARM) | Nix, Source | ✅ |
| Windows | x64 | Source | ✅ |
| Android | ARM | Docker | ✅ (via Tauri) |
| iOS | ARM | Source | ⚠️ (experimental) |

---

## Uninstallation

### Nix

```bash
# List installed packages
nix profile list

# Remove QNTX (replace <index> with actual index from list)
nix profile remove <index>
```

### Docker

```bash
# Remove image
docker rmi ghcr.io/teranos/qntx:latest

# Remove all QNTX images
docker images | grep qntx | awk '{print $3}' | xargs docker rmi
```

### Source Install

```bash
# Remove binary
sudo rm /usr/local/bin/qntx
```

---

## Troubleshooting

### Nix: "experimental features" error

Enable flakes in your Nix configuration:

```bash
mkdir -p ~/.config/nix
echo "experimental-features = nix-command flakes" >> ~/.config/nix/nix.conf
```

### Nix: Slow downloads

If you declined the binary cache prompt, re-run with:

```bash
nix profile install github:teranos/QNTX --accept-flake-config
```

### Docker: Architecture mismatch

Explicitly specify architecture:

```bash
docker pull --platform linux/amd64 ghcr.io/teranos/qntx:latest
# or
docker pull --platform linux/arm64 ghcr.io/teranos/qntx:latest
```

### Build from source: Missing Rust

If `make cli` fails due to missing Rust:

```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Or use pure Go build
make cli-nocgo
```

---

## Next Steps

After installing QNTX:

1. **Configuration**: See [Configuration Guide](../am/README.md)
2. **Development**: See [Nix Development Guide](./nix-development.md)
3. **Segments**: Explore QNTX components in the [main README](../README.md#segments)

---

## Getting Help

- GitHub Issues: https://github.com/teranos/QNTX/issues
- Documentation: https://github.com/teranos/QNTX/tree/main/docs
