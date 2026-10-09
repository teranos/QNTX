package services

import (
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// "zero means zero"

// A plugin's query names how many rows it wants. One naming none is refused,
// not handed every row; 0 is 0 rows; every row is ats.EveryRow, said.
func TestAPluginQueryNamesHowManyRows(t *testing.T) {
	if _, err := protoToFilter(&protocol.AttestationFilter{Predicates: []string{"noted"}}); err == nil {
		t.Fatal("a plugin query naming no limit was read as some number of rows")
	}

	zero := int32(0)
	filter, err := protoToFilter(&protocol.AttestationFilter{Limit: &zero})
	if err != nil || filter.Limit != 0 {
		t.Fatalf("a limit of 0 read as %d, %v", filter.Limit, err)
	}

	every := int32(ats.EveryRow)
	filter, err = protoToFilter(&protocol.AttestationFilter{Limit: &every})
	if err != nil || filter.Limit != ats.EveryRow {
		t.Fatalf("every row read as %d, %v", filter.Limit, err)
	}
}
