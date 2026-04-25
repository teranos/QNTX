package server

import (
	appcfg "github.com/teranos/QNTX/am"
)

// setupClusterLabelSchedule is a no-op — cluster labeling is handled by Voor
// via the GetLabelEligibleClusters, SampleClusterTexts, and SetClusterLabel RPCs.
func (s *QNTXServer) setupClusterLabelSchedule(cfg *appcfg.Config) {}
