package grpc

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestFormatBanner_Boot(t *testing.T) {
	info := BannerInfo{
		Name:    "meili",
		Version: "0.4.0",
		Reason:  BannerBoot,
		Roles:   []string{"search-provider"},
		Status:  "MeiliSearch at 10.195.67.11:7700",
		Details: map[string]string{"indexes": "3"},
	}
	banner := FormatBanner(info)
	plain := stripANSI(banner)

	if !strings.Contains(plain, "meili") || !strings.Contains(plain, "0.4.0") {
		t.Errorf("banner should contain name and version, got:\n%s", plain)
	}
	if !strings.Contains(plain, "search-provider") {
		t.Errorf("banner should contain role, got:\n%s", plain)
	}
	if !strings.Contains(plain, "indexes: 3") {
		t.Errorf("banner should contain index count detail, got:\n%s", plain)
	}
	// Boot reason should not add parenthetical
	if strings.Contains(plain, "(boot)") || strings.Contains(plain, "(restart)") {
		t.Errorf("boot banner should not have reason parenthetical, got:\n%s", plain)
	}
}

func TestFormatBanner_Restart(t *testing.T) {
	info := BannerInfo{
		Name:    "meili",
		Version: "0.4.0",
		Reason:  BannerRestart,
		Roles:   []string{"search-provider"},
		Status:  "MeiliSearch at 10.195.67.11:7700",
	}
	banner := FormatBanner(info)

	if !strings.Contains(banner, "(restart)") {
		t.Errorf("banner should contain (restart), got:\n%s", banner)
	}
}

func TestFormatBanner_Recovered(t *testing.T) {
	info := BannerInfo{
		Name:    "meili",
		Version: "0.4.0",
		Reason:  BannerRecovered,
		Roles:   []string{"search-provider"},
		Status:  "MeiliSearch at 10.195.67.11:7700",
	}
	banner := FormatBanner(info)

	if !strings.Contains(banner, "(recovered)") {
		t.Errorf("banner should contain (recovered), got:\n%s", banner)
	}
}

func TestFormatBanner_Reconfigured(t *testing.T) {
	info := BannerInfo{
		Name:    "meili",
		Version: "0.4.0",
		Reason:  BannerReconfigured,
		Roles:   []string{"search-provider"},
		Status:  "MeiliSearch at 10.195.67.22:7700",
		ConfigDiff: []string{
			"url changed: 10.195.67.11 → 10.195.67.22",
		},
	}
	banner := FormatBanner(info)

	if !strings.Contains(banner, "(reconfigured)") {
		t.Errorf("banner should contain (reconfigured), got:\n%s", banner)
	}
	if !strings.Contains(banner, "url changed") {
		t.Errorf("banner should contain config diff, got:\n%s", banner)
	}
}

func TestFormatBanner_Failed(t *testing.T) {
	info := BannerInfo{
		Name:    "meili",
		Version: "0.4.0",
		Reason:  BannerBoot,
		Error:   "MeiliSearch at 10.195.67.11:7700 not accessible",
	}
	banner := FormatBanner(info)

	if !strings.Contains(banner, "✗") {
		t.Errorf("failed banner should contain ✗, got:\n%s", banner)
	}
	if !strings.Contains(banner, "not accessible") {
		t.Errorf("failed banner should contain error message, got:\n%s", banner)
	}
	// Should NOT contain role lines
	if strings.Contains(banner, "search-provider") {
		t.Errorf("failed banner should not contain role lines, got:\n%s", banner)
	}
}

func TestFormatBanner_HandlersSchedulesWatchers(t *testing.T) {
	info := BannerInfo{
		Name:      "python",
		Version:   "1.0.0",
		Handlers:  []string{"ingest", "process"},
		Schedules: 3,
		Watchers:  1,
	}
	banner := FormatBanner(info)

	if !strings.Contains(banner, "2 handlers") {
		t.Errorf("banner should show handler count, got:\n%s", banner)
	}
	if !strings.Contains(banner, "3 schedules") {
		t.Errorf("banner should show schedule count, got:\n%s", banner)
	}
	if !strings.Contains(banner, "1 watchers") {
		t.Errorf("banner should show watcher count, got:\n%s", banner)
	}
}

func TestDiffConfig_Changes(t *testing.T) {
	before := map[string]string{
		"url": "http://old:7700",
		"key": "abc",
	}
	after := map[string]string{
		"url":   "http://new:7700",
		"key":   "abc",
		"extra": "val",
	}
	diffs := DiffConfig(before, after)

	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d: %v", len(diffs), diffs)
	}

	// Should have "extra added" and "url changed"
	found := map[string]bool{}
	for _, d := range diffs {
		if strings.Contains(d, "extra added") {
			found["added"] = true
		}
		if strings.Contains(d, "url changed") {
			found["changed"] = true
		}
	}
	if !found["added"] {
		t.Errorf("missing 'extra added' diff in %v", diffs)
	}
	if !found["changed"] {
		t.Errorf("missing 'url changed' diff in %v", diffs)
	}
}

func TestDiffConfig_Removed(t *testing.T) {
	before := map[string]string{"url": "http://old:7700", "key": "abc"}
	after := map[string]string{"url": "http://old:7700"}
	diffs := DiffConfig(before, after)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %v", len(diffs), diffs)
	}
	if !strings.Contains(diffs[0], "key removed") {
		t.Errorf("expected 'key removed', got %s", diffs[0])
	}
}

func TestDiffConfig_NilMaps(t *testing.T) {
	diffs := DiffConfig(nil, map[string]string{"a": "b"})
	if diffs != nil {
		t.Errorf("expected nil for nil before, got %v", diffs)
	}
	diffs = DiffConfig(map[string]string{"a": "b"}, nil)
	if diffs != nil {
		t.Errorf("expected nil for nil after, got %v", diffs)
	}
}

func TestColorForPlugin_Deterministic(t *testing.T) {
	c1 := colorForPlugin("meili")
	c2 := colorForPlugin("meili")
	if c1 != c2 {
		t.Errorf("color should be deterministic, got %q and %q", c1, c2)
	}

	// Different names should (usually) get different colors
	c3 := colorForPlugin("python")
	// We can't guarantee different, but at least it shouldn't crash
	_ = c3
}

func TestAccumulator_SetEmit(t *testing.T) {
	acc := NewPluginAccumulator(nil)
	acc.SetLoading("meili", "0.4.0")
	acc.SetRoles("meili", []string{"search-provider"})
	acc.SetHandlers("meili", []string{"index"}, 1, 2)
	acc.SetHealth("meili", true, "MeiliSearch at localhost:7700", map[string]string{"indexes": "5"})

	// Emit should clear the entry
	acc.Emit("meili", BannerBoot)

	acc.mu.Lock()
	_, exists := acc.plugins["meili"]
	acc.mu.Unlock()
	if exists {
		t.Error("Emit should remove the plugin entry")
	}
}

func TestAccumulator_SetFailed(t *testing.T) {
	acc := NewPluginAccumulator(nil)
	acc.SetLoading("meili", "0.4.0")
	acc.SetFailed("meili", "connection refused")

	acc.mu.Lock()
	info := acc.plugins["meili"]
	acc.mu.Unlock()

	if info.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got %q", info.Error)
	}
}

func TestAccumulator_SnapshotAndDiff(t *testing.T) {
	acc := NewPluginAccumulator(nil)
	acc.SnapshotConfig("meili", map[string]string{"url": "http://old:7700"})
	diffs := acc.ComputeConfigDiff("meili", map[string]string{"url": "http://new:7700"})

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %v", len(diffs), diffs)
	}
	if !strings.Contains(diffs[0], "url changed") {
		t.Errorf("expected url change diff, got %s", diffs[0])
	}
}

func TestPluginLogger_NoVersionPrefix(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	sugar := zap.New(core).Named("spindle").Sugar()

	pl := &pluginLogger{
		logger: sugar,
		level:  "info",
	}

	pl.Write([]byte("[spindle] ATSClient connected to 127.0.0.1:50648\n"))

	if logs.Len() != 1 {
		t.Fatalf("expected 1 log entry, got %d", logs.Len())
	}

	msg := logs.All()[0].Message
	// Must NOT contain a version prefix like "[spindle v0.4.1]"
	if strings.Contains(msg, "[spindle v") {
		t.Errorf("message should not contain version prefix, got: %s", msg)
	}
	// The raw line from the plugin is passed through as-is
	if msg != "[spindle] ATSClient connected to 127.0.0.1:50648" {
		t.Errorf("expected raw plugin line, got: %s", msg)
	}
}
