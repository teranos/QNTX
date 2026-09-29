package server

import (
	"net/http"
	"net/url"
	"sort"
	"time"

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
	q.Set("flyer", "blue")

	target := staandPathPrefix + "market/slug?" + q.Encode()
	r, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "could not build the hit %s", target)
	}

	var names []string
	staandArrival(r, "market", "slug", time.Now()).ProtoReflect().Range(
		func(field protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			names = append(names, string(field.Name()))
			return true
		})
	sort.Strings(names)
	return names, nil
}
