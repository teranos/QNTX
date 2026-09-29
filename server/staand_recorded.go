package server

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// StaandRecorded is the Arrival fields a stand fills when a hit carries
// everything it can carry. It sends the hit through staandArrival and reads what
// came out, so nothing here is written apart from the handler: a field the proto
// declares and the handler never fills is not in it. make parity reads this to
// say what a stand records.
func StaandRecorded() ([]string, error) {
	q := url.Values{}
	q.Set(staandEvent, "signup")
	q.Set(staandPage, "/pricing")
	q.Set(staandVisitor, "visitor-0001")
	q.Set(staandVisit, "visit-0001")
	q.Set(staandRef, "https://news.example/posts/7")
	for _, key := range staandCampaign {
		q.Set(key, "x")
	}
	q.Set(staandProbeParam, "blue")

	a, err := staandHit(q)
	if err != nil {
		return nil, err
	}

	var names []string
	a.ProtoReflect().Range(
		func(field protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			names = append(names, string(field.Name()))
			return true
		})
	sort.Strings(names)
	return names, nil
}

// StaandExtraAttributes is the attributes a stand writes onto an arrival that no
// Arrival field carries. It asks staandAttrs about an arrival that holds nothing
// but its stand, so whatever comes back is what the handler adds of its own.
func StaandExtraAttributes() []string {
	keys := []string{}
	for key := range staandAttrs(&protocol.Arrival{Market: "market", Slug: "slug"}) {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// staandProbeParam is a parameter no stand reserves, so it lands in Params.
const staandProbeParam = "flyer"

// StaandProbeCap is as long a value as a probe tries. A field that keeps this
// much keeps any.
const StaandProbeCap = 1024

// StaandProbe is what a stand does with one Arrival field. Each answer comes from
// sending the handler a hit and reading what it made of it.
type StaandProbe struct {
	// Required is a hit that carries nothing else still fills it.
	Required bool
	// MaxLength is the longest value it keeps, StaandProbeCap when there is no
	// end to it.
	MaxLength int
	// KeepsNonUUID is it keeps a value that is not a UUID.
	KeepsNonUUID bool
	// Timestamp is what it fills reads as a timestamp.
	Timestamp bool
}

func staandHit(q url.Values) (*protocol.Arrival, error) {
	target := staandPathPrefix + "market/slug?" + q.Encode()
	r, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "could not build the hit %s", target)
	}
	return staandArrival(r, "market", "slug", time.Now()), nil
}

// staandFieldValue reads one Arrival field as text. The params are read at the
// one key a probe sets.
func staandFieldValue(a *protocol.Arrival, name string) string {
	if name == "params" {
		return a.Params[staandProbeParam]
	}
	field := a.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(name))
	if field == nil {
		return ""
	}
	return a.ProtoReflect().Get(field).String()
}

// staandProbed is how a value reaches each field a visitor's request can fill.
var staandProbed = func() []struct {
	field string
	carry func(value string) url.Values
} {
	one := func(key string) func(string) url.Values {
		return func(v string) url.Values { return url.Values{key: {v}} }
	}
	probed := []struct {
		field string
		carry func(value string) url.Values
	}{
		{"event", one(staandEvent)},
		{"path", one(staandPage)},
		{"visitor", one(staandVisitor)},
		{"visit", one(staandVisit)},
		{"referrer_domain", func(v string) url.Values { return url.Values{staandRef: {"https://" + v + "/"}} }},
		{"referrer_path", func(v string) url.Values { return url.Values{staandRef: {"https://host.example/" + v}} }},
		{"params", one(staandProbeParam)},
	}
	for _, key := range staandCampaign {
		probed = append(probed, struct {
			field string
			carry func(value string) url.Values
		}{key, one(key)})
	}
	return probed
}()

const staandProbeText = "not-a-uuid-value"

// StaandProbes says, for each Arrival field a stand fills, what it does with a
// value: whether a bare hit fills it, how long a value it keeps, whether it keeps
// one that is not a UUID, and whether it reads as a timestamp. make parity holds
// these against the column each field is in Umami.
func StaandProbes() (map[string]StaandProbe, error) {
	bare, err := staandHit(url.Values{})
	if err != nil {
		return nil, err
	}
	filled := func(name string) bool { return staandFieldValue(bare, name) != "" }
	timestamp := func(name string) bool {
		_, err := time.Parse(time.RFC3339Nano, staandFieldValue(bare, name))
		return err == nil
	}

	probes := map[string]StaandProbe{
		"at": {Required: filled("at"), Timestamp: timestamp("at")},
	}

	// The market and the slug are authored by the caller of staandArrival, and
	// what refuses them is the rule a stand's creation applies.
	for name, ok := range map[string]func(string) bool{"market": staandMarket, "slug": staandSlugOK} {
		p := StaandProbe{Required: filled(name), KeepsNonUUID: ok(staandProbeText)}
		for n := 1; n <= StaandProbeCap; n++ {
			if ok(strings.Repeat("a", n)) {
				p.MaxLength = n
			}
		}
		probes[name] = p
	}

	for _, probed := range staandProbed {
		p := StaandProbe{Required: filled(probed.field), Timestamp: timestamp(probed.field)}

		a, err := staandHit(probed.carry(staandProbeText))
		if err != nil {
			return nil, err
		}
		p.KeepsNonUUID = strings.Contains(staandFieldValue(a, probed.field), staandProbeText)

		for n := 1; n <= StaandProbeCap; n++ {
			a, err := staandHit(probed.carry(strings.Repeat("a", n)))
			if err != nil {
				return nil, err
			}
			p.MaxLength = max(p.MaxLength, len(staandFieldValue(a, probed.field)))
		}
		probes[probed.field] = p
	}
	return probes, nil
}
