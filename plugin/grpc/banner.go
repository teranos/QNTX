package grpc

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/logger"
	"go.uber.org/zap"
)

// BannerReason describes why the banner is being emitted.
type BannerReason string

const (
	BannerBoot         BannerReason = ""
	BannerRestart      BannerReason = "restart"
	BannerRecovered    BannerReason = "recovered"
	BannerReconfigured BannerReason = "reconfigured"
	BannerEnabled      BannerReason = "enabled"
	BannerDisabled     BannerReason = "disabled"
)

// BannerInfo holds accumulated lifecycle data for a single plugin banner.
type BannerInfo struct {
	Name       string
	Version    string
	Reason     BannerReason
	Roles      []string          // "search-provider", "llm-provider", etc.
	Handlers           []string // async handler names
	ScheduleNames      []string // schedule handler names
	WatcherNames       []string // watcher IDs
	UnfilteredWatchers []string // watcher IDs with empty filters (receive all attestations)
	Status     string            // from Health().Message
	Details    map[string]string // from Health().Details
	Error      string            // non-empty = failed
	ConfigDiff []string          // "url changed: old → new"
	HTTPRoutes []string          // "GET /version", "GET /status", etc.

}

// bannerPalette contains 8 distinct ANSI colors for plugin identity.
// Selected for contrast on dark and light terminals.
var bannerPalette = []string{
	"\033[36m", // cyan
	"\033[33m", // yellow
	"\033[35m", // magenta
	"\033[32m", // green
	"\033[34m", // blue
	"\033[91m", // bright red
	"\033[93m", // bright yellow
	"\033[96m", // bright cyan
}

const (
	ansiReset = "\033[0m"
	ansiRed   = "\033[31m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
)

// bannerWidth is the total width of the header line.
const bannerWidth = 52

// colorForPlugin returns a deterministic ANSI color for a plugin name.
func colorForPlugin(name string) string {
	h := uint32(0)
	for _, c := range name {
		h = h*31 + uint32(c)
	}
	return bannerPalette[h%uint32(len(bannerPalette))]
}

// FormatBanner renders a multi-line ANSI-colored banner string for a plugin.
func FormatBanner(info BannerInfo) string {
	color := colorForPlugin(info.Name)
	var b strings.Builder

	// Header line: HH:MM:SS ── name version (reason) ───────────
	ts := time.Now().Format("15:04:05")
	reason := ""
	if info.Reason != "" {
		reason = " (" + string(info.Reason) + ")"
	}
	// Plain length for padding calculation (no ANSI)
	plainPrefix := ts + " ── " + info.Name + " " + info.Version + reason + " "
	padLen := bannerWidth - len(plainPrefix)
	if padLen < 3 {
		padLen = 3
	}
	pad := strings.Repeat("─", padLen)

	// Colored: timestamp dim, ── in plugin color, name bold, version dim
	b.WriteString(ansiDim)
	b.WriteString(ts)
	b.WriteString(ansiReset)
	b.WriteString(" ")
	b.WriteString(color)
	b.WriteString("── ")
	b.WriteString(ansiBold)
	b.WriteString(info.Name)
	b.WriteString(ansiReset)
	b.WriteString(" ")
	b.WriteString(ansiDim)
	b.WriteString(info.Version)
	if reason != "" {
		b.WriteString(reason)
	}
	b.WriteString(ansiReset)
	b.WriteString(" ")
	b.WriteString(color)
	b.WriteString(pad)
	b.WriteString(ansiReset)
	b.WriteByte('\n')

	if info.Error != "" {
		// Error banner
		b.WriteString("   ")
		b.WriteString(ansiRed)
		b.WriteString("✗ ")
		b.WriteString(info.Error)
		b.WriteString(ansiReset)
		b.WriteByte('\n')
		return b.String()
	}

	// Role + status lines
	for _, role := range info.Roles {
		b.WriteString("   ")
		b.WriteString(color)
		b.WriteString(role)
		b.WriteString(ansiReset)
		if info.Status != "" {
			b.WriteString("  ")
			b.WriteString(info.Status)
		}
		b.WriteByte('\n')
	}

	// If no roles but there's a status, show it standalone
	if len(info.Roles) == 0 && info.Status != "" {
		b.WriteString("   ")
		b.WriteString(info.Status)
		b.WriteByte('\n')
	}

	// Detail lines from Health().Details
	if len(info.Details) > 0 {
		keys := make([]string, 0, len(info.Details))
		for k := range info.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			// Skip details already represented in status
			if k == "backend" {
				continue
			}
			if info.Status != "" && strings.Contains(info.Status, info.Details[k]) {
				continue
			}
			v := info.Details[k]
			if v == "false" {
				continue
			}
			b.WriteString("   ")
			b.WriteString(ansiDim)
			if v == "true" {
				b.WriteString(k)
			} else {
				b.WriteString(k)
				b.WriteString(": ")
				b.WriteString(v)
			}
			b.WriteString(ansiReset)
			b.WriteByte('\n')
		}
	}

	// Summary of handlers/schedules/watchers
	var counts []string
	if n := len(info.Handlers); n > 0 {
		if n <= 10 {
			counts = append(counts, fmt.Sprintf("%d handlers: %s", n, strings.Join(info.Handlers, ", ")))
		} else {
			counts = append(counts, fmt.Sprintf("%d handlers: %s, …", n, strings.Join(info.Handlers[:10], ", ")))
		}
	}
	if n := len(info.ScheduleNames); n > 0 {
		if n <= 10 {
			counts = append(counts, fmt.Sprintf("%d schedules: %s", n, strings.Join(info.ScheduleNames, ", ")))
		} else {
			counts = append(counts, fmt.Sprintf("%d schedules: %s, …", n, strings.Join(info.ScheduleNames[:10], ", ")))
		}
	}
	if n := len(info.WatcherNames); n > 0 {
		suffix := ""
		if u := len(info.UnfilteredWatchers); u > 0 {
			suffix = fmt.Sprintf(" (%d unfiltered)", u)
		}
		if n <= 10 {
			counts = append(counts, fmt.Sprintf("%d watchers: %s%s", n, strings.Join(info.WatcherNames, ", "), suffix))
		} else {
			counts = append(counts, fmt.Sprintf("%d watchers: %s, …%s", n, strings.Join(info.WatcherNames[:10], ", "), suffix))
		}
	}
	if len(counts) > 0 {
		b.WriteString("   ")
		b.WriteString(ansiDim)
		b.WriteString(strings.Join(counts, ", "))
		b.WriteString(ansiReset)
		b.WriteByte('\n')
	}

	// HTTP routes
	if len(info.HTTPRoutes) > 0 {
		b.WriteString("   ")
		b.WriteString(ansiDim)
		b.WriteString(strings.Join(info.HTTPRoutes, ", "))
		b.WriteString(ansiReset)
		b.WriteByte('\n')
	} else if info.Error == "" && info.Reason != BannerDisabled {
		b.WriteString("   ")
		b.WriteString(ansiDim)
		b.WriteString("no routes advertised (set http_routes in InitializeResponse)")
		b.WriteString(ansiReset)
		b.WriteByte('\n')
	}

	// Config diff lines
	for _, diff := range info.ConfigDiff {
		b.WriteString("   ")
		b.WriteString(ansiDim)
		b.WriteString(diff)
		b.WriteString(ansiReset)
		b.WriteByte('\n')
	}

	return b.String()
}

// DiffConfig compares two config maps and returns human-readable diff lines.
func DiffConfig(before, after map[string]string) []string {
	if before == nil || after == nil {
		return nil
	}

	allKeys := make(map[string]bool)
	for k := range before {
		allKeys[k] = true
	}
	for k := range after {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var diffs []string
	for _, k := range keys {
		oldVal, hadOld := before[k]
		newVal, hasNew := after[k]
		if hadOld && hasNew && oldVal != newVal {
			diffs = append(diffs, fmt.Sprintf("%s changed: %s → %s", k, oldVal, newVal))
		} else if hadOld && !hasNew {
			diffs = append(diffs, fmt.Sprintf("%s removed", k))
		} else if !hadOld && hasNew {
			diffs = append(diffs, fmt.Sprintf("%s added: %s", k, newVal))
		}
	}
	return diffs
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until we find a letter (the terminator)
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				j++ // skip the terminator letter
			}
			i = j
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// PluginAccumulator collects lifecycle events per plugin and emits banners.
type PluginAccumulator struct {
	mu       sync.Mutex
	plugins  map[string]*BannerInfo
	previous map[string]map[string]string // config snapshots for diff
	logger   *zap.SugaredLogger
}

// NewPluginAccumulator creates a new accumulator.
func NewPluginAccumulator(logger *zap.SugaredLogger) *PluginAccumulator {
	return &PluginAccumulator{
		plugins:  make(map[string]*BannerInfo),
		previous: make(map[string]map[string]string),
		logger:   logger,
	}
}

func (a *PluginAccumulator) getOrCreate(name string) *BannerInfo {
	if info, ok := a.plugins[name]; ok {
		return info
	}
	info := &BannerInfo{Name: name}
	a.plugins[name] = info
	return info
}

// SetLoading creates an entry when a plugin starts loading.
func (a *PluginAccumulator) SetLoading(name, version string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.Version = version
}

// SetRoles records provider roles after Initialize response.
func (a *PluginAccumulator) SetRoles(name string, roles []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.Roles = roles
}

// SetHandlers records handler/schedule/watcher names after registration.
func (a *PluginAccumulator) SetHandlers(name string, handlers, scheduleNames, watcherNames, unfilteredWatcherNames []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.Handlers = handlers
	info.ScheduleNames = scheduleNames
	info.WatcherNames = watcherNames
	info.UnfilteredWatchers = unfilteredWatcherNames
}

// SetHTTPRoutes records the HTTP routes a plugin advertised.
func (a *PluginAccumulator) SetHTTPRoutes(name string, routes []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.HTTPRoutes = routes
}

// SetHealth records health check results.
func (a *PluginAccumulator) SetHealth(name string, healthy bool, message string, details map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.Status = message
	info.Details = details
	if !healthy && message != "" {
		info.Error = message
	}
}

// SetFailed records a lifecycle failure.
func (a *PluginAccumulator) SetFailed(name string, err string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.Error = err
}

// SnapshotConfig saves the current config for later diffing.
func (a *PluginAccumulator) SnapshotConfig(name string, config map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := make(map[string]string, len(config))
	for k, v := range config {
		snap[k] = v
	}
	a.previous[name] = snap
}

// ComputeConfigDiff computes the diff between the snapshot and current config.
func (a *PluginAccumulator) ComputeConfigDiff(name string, current map[string]string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	prev, ok := a.previous[name]
	if !ok {
		return nil
	}
	return DiffConfig(prev, current)
}

// SetConfigDiff sets the config diff lines on the accumulator entry.
func (a *PluginAccumulator) SetConfigDiff(name string, diffs []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.getOrCreate(name)
	info.ConfigDiff = diffs
}

// Emit builds the final BannerInfo, prints the banner to stderr, logs structured data, and resets the entry.
func (a *PluginAccumulator) Emit(name string, reason BannerReason) {
	a.mu.Lock()
	info, ok := a.plugins[name]
	if !ok {
		a.mu.Unlock()
		return
	}
	info.Reason = reason
	// Copy and remove from map
	infoCopy := *info
	delete(a.plugins, name)
	a.mu.Unlock()

	// Render banner — colored for terminal, plain for log file
	banner := FormatBanner(infoCopy)
	plain := stripANSI(banner)
	logger.WriteRaw(banner, plain)
}
