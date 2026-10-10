package services

import (
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// "zero means zero"

// A plugin's query names how many rows it wants: 0 is 0 rows; every row is
// ats.EveryRow, said. It names where its time range starts and ends, or that
// it is unbounded there; naming neither is refused, not read as unbounded.
func TestAPluginQueryNamesHowManyRowsAndItsTimeRange(t *testing.T) {
	unbounded := func(limit int32) *protocol.AttestationFilter {
		return &protocol.AttestationFilter{
			Limit: limit,
			From:  &protocol.AttestationFilter_UnboundedStart{UnboundedStart: true},
			To:    &protocol.AttestationFilter_UnboundedEnd{UnboundedEnd: true},
		}
	}

	filter, err := protoToFilter(unbounded(0))
	if err != nil || filter.Limit != 0 {
		t.Fatalf("a limit of 0 read as %d, %v", filter.Limit, err)
	}

	filter, err = protoToFilter(unbounded(ats.EveryRow))
	if err != nil || filter.Limit != ats.EveryRow {
		t.Fatalf("every row read as %d, %v", filter.Limit, err)
	}
	if filter.TimeStart != nil || filter.TimeEnd != nil {
		t.Fatalf("an unbounded range read as %v to %v", filter.TimeStart, filter.TimeEnd)
	}

	bounded := unbounded(ats.EveryRow)
	bounded.From = &protocol.AttestationFilter_TimeStart{TimeStart: 1000}
	bounded.To = &protocol.AttestationFilter_TimeEnd{TimeEnd: 2000}
	filter, err = protoToFilter(bounded)
	if err != nil || filter.TimeStart.UnixMilli() != 1000 || filter.TimeEnd.UnixMilli() != 2000 {
		t.Fatalf("a bounded range read as %v to %v, %v", filter.TimeStart, filter.TimeEnd, err)
	}

	for name, unsaid := range map[string]func(*protocol.AttestationFilter){
		"no start":              func(f *protocol.AttestationFilter) { f.From = nil },
		"no end":                func(f *protocol.AttestationFilter) { f.To = nil },
		"unbounded_start false": func(f *protocol.AttestationFilter) { f.From = &protocol.AttestationFilter_UnboundedStart{} },
		"unbounded_end false":   func(f *protocol.AttestationFilter) { f.To = &protocol.AttestationFilter_UnboundedEnd{} },
	} {
		f := unbounded(1)
		unsaid(f)
		if _, err := protoToFilter(f); err == nil {
			t.Errorf("%s: read as some range", name)
		}
	}
}
