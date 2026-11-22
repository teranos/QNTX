package grpc

import (
	"strconv"
	"strings"
)

// "so some plugins, or maybe even all, are more like workoalds we treat somewhat equally,"
// "so its like a deprioritised plugin, they will run, but they will get out of the way more than they want to be active,"
// "so its a way to utlize unused compute if you will."
//
// 「[Opus 5.5] A plugin whose record says yield = true runs on what the machine has spare: the」
// 「[Opus 5.5] node starts it at the lowest priority for the CPU and the disk, and, where the」
// 「[Opus 5.5] kernel says how pressed its memory is, stops it while memory is pressed and」
// 「[Opus 5.5] starts it again once it is not. Its record is not written either way.」

// PluginYieldKey is the key of a plugin's record that says it yields. QNTX's,
// like namespace and the build keys: the plugin does not validate it.
const PluginYieldKey = "yield"

// Yields is whether the record of the plugin called name says it yields.
func Yields(name string) bool {
	record, found, err := pluginRecord(name)
	if err != nil || !found {
		return false
	}
	yields, err := strconv.ParseBool(strings.TrimSpace(record.Config[PluginYieldKey]))
	return err == nil && yields
}

// GiveWay puts every running plugin that yields at the lowest priority again,
// so threads it started since are at it too. It names each it could not.
func (m *PluginManager) GiveWay() map[string]error {
	m.mu.RLock()
	pids := map[string]int{}
	for name, p := range m.plugins {
		if launched, ok := p.proc.(*launchedPlugin); ok && launched.cmd.Process != nil && Yields(name) {
			pids[name] = launched.cmd.Process.Pid
		}
	}
	m.mu.RUnlock()
	failed := map[string]error{}
	for name, pid := range pids {
		if err := giveWay(pid); err != nil {
			failed[name] = err
		}
	}
	return failed
}

// Yielding is the plugins running now whose records say they yield.
func (m *PluginManager) Yielding() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for name := range m.plugins {
		if Yields(name) {
			names = append(names, name)
		}
	}
	return names
}
