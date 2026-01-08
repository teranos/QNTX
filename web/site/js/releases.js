// Fetch and display GitHub release downloads
// Fetches from GitHub API client-side to get latest release assets

(function() {
    'use strict';

    const GITHUB_REPO = 'teranos/QNTX';
    const API_URL = `https://api.github.com/repos/${GITHUB_REPO}/releases`;

    document.addEventListener('DOMContentLoaded', init);

    async function init() {
        const fullContainer = document.getElementById('release-downloads');
        const latestContainer = document.getElementById('latest-release');

        if (!fullContainer && !latestContainer) return;

        try {
            const response = await fetch(API_URL);
            if (!response.ok) {
                throw new Error(`GitHub API error: ${response.status}`);
            }

            const releases = await response.json();
            if (!releases.length) {
                if (fullContainer) fullContainer.innerHTML = '<p>No releases available yet.</p>';
                if (latestContainer) latestContainer.innerHTML = '<p>No releases available yet.</p>';
                return;
            }

            if (fullContainer) {
                renderReleases(fullContainer, releases);
            }

            if (latestContainer) {
                renderLatestRelease(latestContainer, releases);
            }
        } catch (error) {
            console.error('Failed to fetch releases:', error);
            const errorHtml = `<p class="error">Unable to load releases. <a href="https://github.com/${GITHUB_REPO}/releases">View on GitHub</a></p>`;
            if (fullContainer) fullContainer.innerHTML = errorHtml;
            if (latestContainer) latestContainer.innerHTML = errorHtml;
        }
    }

    function renderLatestRelease(container, releases) {
        // Find latest non-draft release
        const latest = releases.find(r => !r.draft);
        if (!latest) {
            container.innerHTML = '<p>No releases available yet.</p>';
            return;
        }

        const assets = groupAssetsByPlatform(latest.assets);
        const prerelease = latest.prerelease ? ' <span class="prerelease-badge">Pre-release</span>' : '';

        container.innerHTML = `
            <div class="release-version latest">
                <div class="release-header">
                    <h3>${escapeHtml(latest.tag_name)}${prerelease} <span class="latest-badge">Latest</span></h3>
                    <span class="release-date">${formatDate(latest.published_at)}</span>
                </div>
                <ul class="platform-list">
                    ${renderPlatformLinks(assets)}
                </ul>
            </div>
        `;
    }

    function renderReleases(container, releases) {
        // Group releases by minor version (e.g., v1.0, v1.1)
        const latestByMinor = new Map();

        for (const release of releases) {
            if (release.draft) continue;

            const version = parseVersion(release.tag_name);
            if (!version) continue;

            const minorKey = `${version.major}.${version.minor}`;
            if (!latestByMinor.has(minorKey)) {
                latestByMinor.set(minorKey, release);
            }
        }

        // Sort by version descending
        const sortedVersions = Array.from(latestByMinor.entries())
            .sort((a, b) => {
                const [aMajor, aMinor] = a[0].split('.').map(Number);
                const [bMajor, bMinor] = b[0].split('.').map(Number);
                if (bMajor !== aMajor) return bMajor - aMajor;
                return bMinor - aMinor;
            });

        if (sortedVersions.length === 0) {
            container.innerHTML = '<p>No releases available yet.</p>';
            return;
        }

        let html = '';

        for (const [minorVersion, release] of sortedVersions) {
            const assets = groupAssetsByPlatform(release.assets);
            const isLatest = sortedVersions[0][1] === release;
            const prerelease = release.prerelease ? ' <span class="prerelease-badge">Pre-release</span>' : '';

            html += `
                <div class="release-version ${isLatest ? 'latest' : ''}">
                    <div class="release-header">
                        <h3>${escapeHtml(release.tag_name)}${prerelease}${isLatest ? ' <span class="latest-badge">Latest</span>' : ''}</h3>
                        <span class="release-date">${formatDate(release.published_at)}</span>
                    </div>
                    <ul class="platform-list">
                        ${renderPlatformLinks(assets)}
                    </ul>
                </div>
            `;
        }

        container.innerHTML = html;
    }

    function parseVersion(tag) {
        // Parse version tags like v1.0.0, v1.0.0-beta.1
        const match = tag.match(/^v?(\d+)\.(\d+)\.?(\d+)?/);
        if (!match) return null;
        return {
            major: parseInt(match[1], 10),
            minor: parseInt(match[2], 10),
            patch: parseInt(match[3] || '0', 10)
        };
    }

    function groupAssetsByPlatform(assets) {
        const platforms = {
            'linux-amd64': { name: 'Linux x86_64', icon: '🐧', assets: [] },
            'linux-arm64': { name: 'Linux ARM64', icon: '🐧', assets: [] },
            'darwin-amd64': { name: 'macOS Intel', icon: '🍎', assets: [] },
            'darwin-arm64': { name: 'macOS Apple Silicon', icon: '🍎', assets: [] },
            'windows-amd64': { name: 'Windows x86_64', icon: '🪟', assets: [] },
            'other': { name: 'Other', icon: '📦', assets: [] }
        };

        for (const asset of assets) {
            const name = asset.name.toLowerCase();
            let matched = false;

            // Match platform patterns
            if (name.includes('linux') && name.includes('amd64')) {
                platforms['linux-amd64'].assets.push(asset);
                matched = true;
            } else if (name.includes('linux') && (name.includes('arm64') || name.includes('aarch64'))) {
                platforms['linux-arm64'].assets.push(asset);
                matched = true;
            } else if ((name.includes('darwin') || name.includes('macos')) && name.includes('amd64')) {
                platforms['darwin-amd64'].assets.push(asset);
                matched = true;
            } else if ((name.includes('darwin') || name.includes('macos')) && (name.includes('arm64') || name.includes('aarch64'))) {
                platforms['darwin-arm64'].assets.push(asset);
                matched = true;
            } else if (name.includes('windows') && name.includes('amd64')) {
                platforms['windows-amd64'].assets.push(asset);
                matched = true;
            }

            // Skip checksums and signatures for 'other'
            if (!matched && !name.endsWith('.sha256') && !name.endsWith('.sig') && !name.endsWith('.txt')) {
                platforms['other'].assets.push(asset);
            }
        }

        return platforms;
    }

    function renderPlatformLinks(platforms) {
        let html = '';

        for (const [key, platform] of Object.entries(platforms)) {
            if (platform.assets.length === 0) continue;

            // Get primary download (prefer .tar.gz or .zip)
            const primary = platform.assets.find(a =>
                a.name.endsWith('.tar.gz') || a.name.endsWith('.zip')
            ) || platform.assets[0];

            const size = formatSize(primary.size);

            html += `
                <li>
                    <span class="platform-name">${platform.icon} ${escapeHtml(platform.name)}</span>
                    <span class="platform-link">
                        <a href="${escapeHtml(primary.browser_download_url)}" class="download-link">${escapeHtml(primary.name)}</a>
                        <span class="file-size">(${size})</span>
                    </span>
                </li>
            `;
        }

        return html || '<li>No downloads available</li>';
    }

    function formatDate(dateString) {
        const date = new Date(dateString);
        return date.toLocaleDateString('en-US', {
            year: 'numeric',
            month: 'short',
            day: 'numeric'
        });
    }

    function formatSize(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    }

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
})();
