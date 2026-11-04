package a2a

import (
	"strings"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Lacking is every field a request leaves empty that the spec requires (§3.3.2,
// §5.7), read as the pinned message name declares it from its ProtoJSON raw,
// each named from inside the request: message.message_id.
func Lacking(name protoreflect.FullName, raw []byte) ([]string, error) {
	descriptor, err := message(name)
	if err != nil {
		return nil, err
	}
	request := dynamicpb.NewMessage(descriptor)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, request); err != nil {
		return nil, errors.Wrapf(err, "the request is not an %s", name)
	}
	missing := Missing(request)
	for i, path := range missing {
		missing[i] = strings.TrimPrefix(path, string(name.Name())+".")
	}
	return missing, nil
}
