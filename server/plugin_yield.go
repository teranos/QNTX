package server

import (
	"context"
	"time"

	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
)

// "so its like a deprioritised plugin, they will run, but they will get out of the way more than they want to be active,"
// "so its a way to utlize unused compute if you will."
//
// 「[Opus 5.5] Every few seconds the node puts its yielding plugins' threads at the lowest」
// 「[Opus 5.5] priority again, and reads how pressed its memory is. Pressed for long enough,」
// 「[Opus 5.5] it stops them: a priority frees no memory, and on 2026-11-22 a build, a query and」
// 「[Opus 5.5] gaze's model together took k. Calm for long enough, it starts them again. A」
// 「[Opus 5.5] plugin's record is not written: one switched off meanwhile stays off.」

var (
	yieldEvery = 5 * time.Second
	// Pressed: some task waited for memory this share of the last ten seconds,
	// twice in a row.
	yieldPressed      = 10.0
	yieldPressedTimes = 2
	// Calm: under this share over the last minute, for this long.
	yieldCalm    = 1.0
	yieldCalmFor = 2 * time.Minute
)

// yieldLoop runs for as long as the node does.
func (s *QNTXServer) yieldLoop() {
	stops := s.stopping()
	gaveWay := map[string]time.Time{}
	pressed, calmSince := 0, time.Time{}
	for {
		select {
		case <-stops.Done():
			return
		case <-time.After(yieldEvery):
		}
		pm, err := s.getPluginManager()
		if err != nil {
			continue
		}
		for name, err := range pm.GiveWay() {
			s.logger.Warnw("A plugin that yields is not at the lowest priority", "plugin", name, "error", err)
		}
		avg10, avg60, said := plugingrpc.MemoryPressure()
		if !said {
			continue
		}
		if avg10 >= yieldPressed {
			pressed++
		} else {
			pressed = 0
		}
		if pressed >= yieldPressedTimes {
			for _, name := range pm.Yielding() {
				if err := pm.DisablePlugin(stops, name, s.pluginRegistry); err != nil {
					s.logger.Errorw("A plugin that yields did not stop for memory pressure", "plugin", name, "pressure_avg10", avg10, "error", err)
					continue
				}
				s.pluginMuxes.Delete(name)
				s.pluginMuxInit.Delete(name)
				gaveWay[name] = time.Now()
				s.logger.Warnw("A plugin gave way to memory pressure", "plugin", name, "pressure_avg10", avg10, "pressure_avg60", avg60)
			}
			calmSince = time.Time{}
			continue
		}
		if len(gaveWay) == 0 {
			continue
		}
		if avg60 >= yieldCalm {
			calmSince = time.Time{}
			continue
		}
		if calmSince.IsZero() {
			calmSince = time.Now()
		}
		if time.Since(calmSince) < yieldCalmFor {
			continue
		}
		for name, since := range gaveWay {
			delete(gaveWay, name)
			s.backFromGivingWay(stops, pm, name, since)
		}
		calmSince = time.Time{}
	}
}

// backFromGivingWay starts a plugin that gave way again, unless its record was
// switched off meanwhile.
func (s *QNTXServer) backFromGivingWay(ctx context.Context, pm *plugingrpc.PluginManager, name string, since time.Time) {
	record, found, err := s.pluginRecords().Plugin(name)
	if err != nil || !found || !record.Enabled {
		s.logger.Infow("A plugin that gave way to memory pressure is not started again: its record is off", "plugin", name, "error", err)
		return
	}
	if err := pm.EnablePlugin(ctx, name, pluginSearchPaths(), s.pluginRegistry, s.services); err != nil {
		s.logger.Errorw("A plugin that gave way to memory pressure did not start again", "plugin", name, "error", err)
		s.pluginRegistry.MarkFailed(name, err.Error())
		return
	}
	s.pluginMuxes.Delete(name)
	s.pluginMuxInit.Delete(name)
	s.logger.Infow("A plugin that gave way to memory pressure runs again", "plugin", name, "gave_way_for", time.Since(since).Round(time.Second).String())
}
