package version

import (
	"fmt"
	"runtime"
)

// Build information. These variables are set at build time via ldflags.
var (
	// CommitHash is the git commit hash when the binary was built
	CommitHash = "dev"

	// BuildTime is when the binary was built
	BuildTime = "unknown"

	// Version is the semantic version (if tagged)
	Version = "dev"
)

// Info contains version and build information
type Info struct {
	CommitHash string `json:"commit_hash"`
	BuildTime  string `json:"build_time"`
	Version    string `json:"version"`
	GoVersion  string `json:"go_version"`
	Platform   string `json:"platform"`
}

// Get returns the current version information
func Get() Info {
	return Info{
		CommitHash: CommitHash,
		BuildTime:  BuildTime,
		Version:    Version,
		GoVersion:  runtime.Version(),
		Platform:   fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a human-readable version string
func (i Info) String() string {
	if i.Version != "dev" {
		return fmt.Sprintf("qntx %s (commit %s, built %s)", i.Version, i.CommitHash, i.BuildTime)
	}
	return fmt.Sprintf("qntx dev (commit %s, built %s)", i.CommitHash, i.BuildTime)
}

// Short returns a short version string with just the commit hash
func (i Info) Short() string {
	if len(i.CommitHash) >= 7 {
		return i.CommitHash[:7]
	}
	return i.CommitHash
}
