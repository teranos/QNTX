package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/parity"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// resolvesTo is the message a full name names, or nil: QNTX's own, or one the
// pinned A2A spec declares, which a card is read through.
func resolvesTo(name string) protoreflect.MessageDescriptor {
	found, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(name))
	if err == nil {
		return found.Descriptor()
	}
	files, refused := parity.Descriptors("a2a")
	if refused != nil {
		return nil
	}
	for _, file := range files {
		if declared := file.Messages().ByName(protoreflect.FullName(name).Name()); declared != nil && string(declared.FullName()) == name {
			return declared
		}
	}
	return nil
}

// A field that says which message it carries names one that exists, on every
// signum the node holds.
func TestAFieldsMessageExists(t *testing.T) {
	for _, signum := range bareNode().signa() {
		for _, s := range signum.GetSigils() {
			for _, field := range s.GetGives() {
				if field.GetMessage() == "" {
					continue
				}
				assert.NotNil(t, resolvesTo(field.GetMessage()),
					"%s %s gives %s as %s, which is no message", signum.GetName(), s.GetName(), field.GetName(), field.GetMessage())
			}
		}
	}
}

// A signum that follows a reference names, for every column, a field of a
// message that exists, and names no column without one.
func TestWhatASignumFollowsIsItsOwnFields(t *testing.T) {
	for _, signum := range bareNode().signa() {
		for _, follows := range signum.GetFollows() {
			require.NotEmpty(t, follows.GetReference(), "%s follows something it does not name", signum.GetName())
			for _, c := range follows.GetColumns() {
				message, field, ok := cutLast(c.GetField(), ".")
				require.True(t, ok, "%s names %q, which is not message and field", signum.GetName(), c.GetField())
				descriptor := resolvesTo(message)
				require.NotNil(t, descriptor, "%s names %s, which is no message", signum.GetName(), message)
				assert.NotNil(t, descriptor.Fields().ByName(protoreflect.Name(field)),
					"%s names %s, which %s does not have", signum.GetName(), field, message)
				model, column, ok := strings.Cut(c.GetColumn(), ".")
				assert.True(t, ok && model != "" && column != "",
					"%s puts %s in %q, which is not model and column", signum.GetName(), c.GetField(), c.GetColumn())
			}
		}
	}
}

// Staands follows Umami and nothing else.
func TestStaandsFollowsUmami(t *testing.T) {
	follows := bareNode().staandsSignum().GetFollows()
	require.Len(t, follows, 1)
	assert.Equal(t, "umami", follows[0].GetReference())

	answered, err := answeredOf(bareNode().staandsSignum())
	require.NoError(t, err)
	visits := sigilOf(t, answered, "visits").sigil
	carried := map[string]string{}
	for _, field := range visits.GetGives() {
		carried[field.GetName()] = field.GetMessage()
	}
	assert.Equal(t, "protocol.Visit", carried["visits"])
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}
