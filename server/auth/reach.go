package auth

import "github.com/teranos/QNTX/internal/admission"

// What a route lets in lives in internal/admission, beside the admission it
// reads.
type Reach = admission.Reach

// Also grants reach to levels beside ROOT (admission.Also).
func Also(levels ...Level) Reach { return admission.Also(levels...) }
