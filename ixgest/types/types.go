package types

import (
	atstypes "github.com/teranos/QNTX/ats/types"
)

// Git entity types
var (
	Commit = atstypes.TypeDef{
		Name:  "commit",
		Label: "Commit",
		Color: "#34495e",
	}

	Author = atstypes.TypeDef{
		Name:  "author",
		Label: "Author",
		Color: "#c0392b",
	}

	Branch = atstypes.TypeDef{
		Name:  "branch",
		Label: "Branch",
		Color: "#16a085",
	}
)
