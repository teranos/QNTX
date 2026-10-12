package a2a

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"

	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// A card is signed with the node's own key (§8.4): Ed25519, which JWS names
// EdDSA (RFC 8037 §3.1), under the key id the node's DID document names its
// key by, so whoever holds the node's did:key holds what verifies it.

// signatureAlgorithm is the JWS alg of an Ed25519 signature (RFC 8037 §3.1).
const signatureAlgorithm = "EdDSA"

// protectedHeader is the JWS Protected Header §8.4.2 asks for: alg, typ and kid.
type protectedHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid"`
}

var b64 = base64.RawURLEncoding

// Sign puts one signature on card, made with key under kid, in place of any
// it carried (§8.4.2).
func Sign(card protoreflect.Message, key ed25519.PrivateKey, kid string) error {
	signatures, err := signaturesOf(card)
	if err != nil {
		return err
	}
	card.Clear(signatures)
	payload, err := signedPayload(card)
	if err != nil {
		return err
	}
	header, err := json.Marshal(protectedHeader{Algorithm: signatureAlgorithm, Type: "JOSE", KeyID: kid})
	if err != nil {
		return errors.Wrap(err, "the card's protected header did not marshal")
	}
	protected := b64.EncodeToString(header)
	signature := ed25519.Sign(key, []byte(protected+"."+b64.EncodeToString(payload)))

	list := card.Mutable(signatures).List()
	signed := list.NewElement().Message()
	if err := setText(signed, "protected", protected); err != nil {
		return err
	}
	if err := setText(signed, "signature", b64.EncodeToString(signature)); err != nil {
		return err
	}
	list.Append(protoreflect.ValueOfMessage(signed))
	return nil
}

// Verify is §8.4.3 for a key the caller already trusts: a signature on card
// made with key, over the card as it is without its signatures. kid is the key
// id that key goes by, and a signature under another is not this key's.
func Verify(card protoreflect.Message, key ed25519.PublicKey, kid string) error {
	signatures, err := signaturesOf(card)
	if err != nil {
		return err
	}
	unsigned := proto.Clone(card.Interface()).ProtoReflect()
	unsigned.Clear(signatures)
	payload, err := signedPayload(unsigned)
	if err != nil {
		return err
	}
	list := card.Get(signatures).List()
	for i := 0; i < list.Len(); i++ {
		held := list.Get(i).Message()
		protected, signature, err := textsOf(held)
		if err != nil {
			return err
		}
		raw, err := b64.DecodeString(protected)
		if err != nil {
			return errors.Wrapf(err, "signature %d's protected header is not base64url", i)
		}
		var header protectedHeader
		if err := json.Unmarshal(raw, &header); err != nil {
			return errors.Wrapf(err, "signature %d's protected header is not JSON", i)
		}
		if header.Algorithm != signatureAlgorithm || header.KeyID != kid {
			continue
		}
		sig, err := b64.DecodeString(signature)
		if err != nil {
			return errors.Wrapf(err, "signature %d is not base64url", i)
		}
		if ed25519.Verify(key, []byte(protected+"."+b64.EncodeToString(payload)), sig) {
			return nil
		}
		return errors.Newf("signature %d under %s does not verify against the card as it is", i, kid)
	}
	return errors.Newf("the card carries no %s signature under %s", signatureAlgorithm, kid)
}

// signedPayload is the card as it is signed: its JSON as protojson writes it,
// which leaves out what the card did not set (§8.4.1 rule 1), canonicalized
// (rule 2). The signatures are not in it (rule 3); the caller clears them.
func signedPayload(card protoreflect.Message) ([]byte, error) {
	written, err := protojson.Marshal(card.Interface())
	if err != nil {
		return nil, errors.Wrap(err, "the card did not marshal to be signed")
	}
	return Canonical(written)
}

// fieldNamed is the field of message called name, when it has one.
func fieldNamed(message protoreflect.MessageDescriptor, name protoreflect.Name) (protoreflect.FieldDescriptor, bool) {
	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		if fields.Get(i).Name() == name {
			return fields.Get(i), true
		}
	}
	return nil, false
}

func signaturesOf(card protoreflect.Message) (protoreflect.FieldDescriptor, error) {
	field, held := fieldNamed(card.Descriptor(), "signatures")
	if !held || !field.IsList() || field.Kind() != protoreflect.MessageKind {
		return nil, errors.Newf("%s has no list of signatures to sign it with", card.Descriptor().FullName())
	}
	return field, nil
}

// textField is the text field of m called name.
func textField(m protoreflect.Message, name protoreflect.Name) (protoreflect.FieldDescriptor, error) {
	field, held := fieldNamed(m.Descriptor(), name)
	if !held || field.Kind() != protoreflect.StringKind {
		return nil, errors.Newf("%s has no %s for a signature's text", m.Descriptor().FullName(), name)
	}
	return field, nil
}

func setText(m protoreflect.Message, name protoreflect.Name, value string) error {
	field, err := textField(m, name)
	if err != nil {
		return err
	}
	m.Set(field, protoreflect.ValueOfString(value))
	return nil
}

// textsOf is a signature's protected header and signature, as written.
func textsOf(m protoreflect.Message) (protected, signature string, err error) {
	protectedField, err := textField(m, "protected")
	if err != nil {
		return "", "", err
	}
	signatureField, err := textField(m, "signature")
	if err != nil {
		return "", "", err
	}
	return m.Get(protectedField).String(), m.Get(signatureField).String(), nil
}
