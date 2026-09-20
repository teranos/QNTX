package server

import (
	"path/filepath"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/net"

	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/pulse/async"
)

// hostGaugeInterval is how often the node looks at the machine under it. The
// same minute qntx.store.unsent is taken on, and far longer than the second
// cpu.Percent spends sampling.
const hostGaugeInterval = time.Minute

// startHostGauges records what the machine has left: processor, memory, disk
// and network. The node already read the first two every ten seconds to decide
// how fast watchers may fire, and kept none of it.
func (s *QNTXServer) startHostGauges() {
	sacred.GoTracked(&s.wg, "server.hostGauges", func() {
		ticker := time.NewTicker(hostGaugeInterval)
		defer ticker.Stop()

		// The first reading has nothing to subtract from, so it sets the
		// baseline and says nothing about the network.
		lastIn, lastOut, lastAt := s.hostNetCounters()

		for {
			select {
			case <-s.ctx.Done():
				return
			case at := <-ticker.C:
				s.gaugeHostPressure()
				s.gaugeHostDisk()
				lastIn, lastOut, lastAt = s.gaugeHostNet(lastIn, lastOut, lastAt, at)
			}
		}
	})
}

// gaugeHostPressure records processor and memory. GetPressure answers -1 for
// either one it could not read, and a -1 on a graph is a lie about a quiet
// machine, so an unreadable number is left unrecorded.
func (s *QNTXServer) gaugeHostPressure() {
	mem, cpu := async.GetPressure()
	if mem >= 0 {
		measure.Gauge(measure.HostMemory, mem)
	}
	if cpu >= 0 {
		measure.Gauge(measure.HostCPU, cpu)
	}
	if mem < 0 || cpu < 0 {
		s.logger.Warnw("The machine under the node did not answer",
			"memory_percent", mem, "cpu_percent", cpu)
	}
}

// gaugeHostDisk records the filesystem the operational database sits on. That
// is the one that stops the node when it fills, whatever else is mounted.
func (s *QNTXServer) gaugeHostDisk() {
	if s.dbPath == "" {
		return
	}

	usage, err := disk.Usage(filepath.Dir(s.dbPath))
	if err != nil {
		s.logger.Warnw("The filesystem holding the database did not answer",
			"path", filepath.Dir(s.dbPath), "error", err)
		return
	}
	measure.Gauge(measure.HostDisk, usage.UsedPercent)
}

// hostNetCounters reads the totals every interface has carried since boot.
// A read that failed answers a zero time, which is what tells the next
// difference it has nothing to subtract from.
func (s *QNTXServer) hostNetCounters() (in, out uint64, at time.Time) {
	counters, err := net.IOCounters(false)
	if err != nil || len(counters) == 0 {
		s.logger.Warnw("The network interfaces did not answer",
			"error", err, "counters", len(counters))
		return 0, 0, time.Time{}
	}
	return counters[0].BytesRecv, counters[0].BytesSent, time.Now()
}

// gaugeHostNet records bytes a second since the last reading and returns the
// new one. Counters reset when the host reboots, so a total that went backwards
// is the machine starting over, not traffic.
func (s *QNTXServer) gaugeHostNet(lastIn, lastOut uint64, lastAt, at time.Time) (uint64, uint64, time.Time) {
	in, out, now := s.hostNetCounters()
	if now.IsZero() {
		return lastIn, lastOut, lastAt
	}
	if lastAt.IsZero() || in < lastIn || out < lastOut {
		return in, out, now
	}

	seconds := now.Sub(lastAt).Seconds()
	if seconds <= 0 {
		return in, out, now
	}

	measure.Gauge(measure.HostNetIn, float64(in-lastIn)/seconds)
	measure.Gauge(measure.HostNetOut, float64(out-lastOut)/seconds)
	return in, out, now
}
