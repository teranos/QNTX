# QNTX GitHub Plugin

GitHub integration for repository event tracking and automation.

## Features

- **Event Polling**: Periodic polling of GitHub API for repository events
- **Attestation Creation**: Creates attestations for:
  - Pull requests merged
  - Releases published
  - Issues opened/closed
  - Pushes to main/master branches
- **Rate Limit Aware**: Respects GitHub API rate limits (5,000 requests/hour with token)

## Configuration

Add to `am.toml`:

```toml
[github]
token = "ghp_..."  # GitHub personal access token
repos = ["teranos/qntx"]  # Repositories to watch
poll_interval = 300  # Poll interval in seconds (default: 5 minutes)
```

## Event Attestations

### Pull Request Merged
- **Subject**: PR URL
- **Predicate**: `pr-merged`
- **Context**: `github`
- **Attributes**: pr_number, pr_title, pr_url, author, base_ref, head_ref, merged_at

### Release Published
- **Subject**: Release URL
- **Predicate**: `released`
- **Context**: `github`
- **Attributes**: tag, name, url, author, body

### Issue Opened/Closed
- **Subject**: Issue URL
- **Predicate**: `issue-opened` or `issue-closed`
- **Context**: `github`
- **Attributes**: issue_number, title, url, author, state

### Push to Main/Master
- **Subject**: Branch URL
- **Predicate**: `pushed`
- **Context**: `github`
- **Attributes**: ref, branch, commit_count, commits, actor

## Usage

The plugin automatically creates attestations for repository events. Query them in the canvas:

```
// Find all merged PRs
subject: * predicate: pr-merged context: github

// Find all releases
subject: * predicate: released context: github
```

Compose with other plugins (e.g., post to Bluesky via qntx-atproto) using ax-py meld.

## Building & Installation

```bash
# Build
cd qntx-github
make build

# Install to ~/.qntx/plugins/
make install

# Or use Nix
nix build ./qntx-github#qntx-github-plugin
```
