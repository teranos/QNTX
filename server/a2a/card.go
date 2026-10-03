package a2a

import (
	"encoding/json"
	"strconv"

	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// AgentCard is lf.a2a.v1.AgentCard, the message a card is held to.
const AgentCard = "lf.a2a.v1.AgentCard"

// Skill is one signum as A2A sees it: "To A2A a signum is a skill" (ADR-039).
type Skill struct {
	ID          string
	Name        string
	Description string
	Tags        []string
	// Bearer says calling the skill takes the node's bearer token, for a card
	// that shows a skill to whoever reads it and not only to who reaches it.
	Bearer bool
}

// Extension is one AgentExtension the card declares (§4.6): what a field of
// the AgentCard has no place for.
type Extension struct {
	URI         string
	Description string
	Params      map[string]any
}

// Modes is the media types the node takes and gives: JSON, everywhere.
var Modes = []string{"application/json"}

// Card is what the node can say of itself to one caller. What it does not
// have it leaves empty, and Missing says so; nothing here is made up to fill a
// field the spec requires.
type Card struct {
	Name        string
	Description string
	Version     string
	// URL is where the HTTP+JSON binding answers, for the interface entry.
	URL        string
	Skills     []Skill
	Extensions []Extension
}

// Message is the card as lf.a2a.v1.AgentCard, read through the pinned spec, so
// it cannot carry what the proto does not declare.
func (c Card) Message() (protoreflect.Message, error) {
	descriptor, err := message(AgentCard)
	if err != nil {
		return nil, err
	}
	skills := []map[string]any{}
	for _, s := range c.Skills {
		tags := s.Tags
		if tags == nil {
			tags = []string{}
		}
		skill := map[string]any{"id": s.ID, "name": s.Name, "description": s.Description, "tags": tags}
		if s.Bearer {
			skill["securityRequirements"] = bearerRequired
		}
		skills = append(skills, skill)
	}
	extensions := []map[string]any{}
	for _, e := range c.Extensions {
		extensions = append(extensions, map[string]any{"uri": e.URI, "description": e.Description, "required": false, "params": e.Params})
	}
	// An interface is a way to speak A2A (§8.3.1), so the node's MCP is not
	// one: where it answers is said in the node extension.
	interfaces := []map[string]any{
		{"url": c.URL, "protocolBinding": "HTTP+JSON", "protocolVersion": Version},
	}
	body, err := json.Marshal(map[string]any{
		"name":                c.Name,
		"description":         c.Description,
		"version":             c.Version,
		"supportedInterfaces": interfaces,
		// What the node does not do is said as false (§3.3.4).
		"capabilities": map[string]any{"streaming": false, "pushNotifications": false, "extensions": extensions},
		// The node's token is a bearer token (§3.1.11: the extended card is
		// authenticated with a scheme the card declares).
		"securitySchemes": map[string]any{
			"bearer": map[string]any{"httpAuthSecurityScheme": map[string]any{"scheme": "Bearer"}},
		},
		"securityRequirements": bearerRequired,
		"defaultInputModes":    Modes,
		"defaultOutputModes":   Modes,
		"skills":               skills,
	})
	if err != nil {
		return nil, errors.Wrap(err, "the card did not marshal")
	}
	card := dynamicpb.NewMessage(descriptor)
	if err := protojson.Unmarshal(body, card); err != nil {
		return nil, errors.Wrapf(err, "the card is not an %s", AgentCard)
	}
	return card, nil
}

// bearerRequired is a SecurityRequirement naming the bearer scheme the card
// declares, with no scopes.
var bearerRequired = []map[string]any{{"schemes": map[string]any{"bearer": map[string]any{"list": []string{}}}}}

// Missing is every field the card leaves empty that the spec requires: a
// REQUIRED field unset or empty, and a REQUIRED list with no element (§5.7).
// A list is named per element, skills[0].id.
func Missing(card protoreflect.Message) []string {
	var missing []string
	walk(card, string(card.Descriptor().Name()), &missing)
	return missing
}

func walk(m protoreflect.Message, at string, missing *[]string) {
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		named := at + "." + string(field.Name())
		value := m.Get(field)
		switch {
		case field.IsList():
			list := value.List()
			if list.Len() == 0 && parity.Required(field) {
				*missing = append(*missing, named)
			}
			if field.Kind() == protoreflect.MessageKind {
				for j := 0; j < list.Len(); j++ {
					walk(list.Get(j).Message(), named+"["+strconv.Itoa(j)+"]", missing)
				}
			}
		case field.IsMap():
			if value.Map().Len() == 0 && parity.Required(field) {
				*missing = append(*missing, named)
			}
		case field.Kind() == protoreflect.MessageKind:
			if !m.Has(field) {
				if parity.Required(field) {
					*missing = append(*missing, named)
				}
				continue
			}
			walk(value.Message(), named, missing)
		default:
			if !m.Has(field) && parity.Required(field) {
				*missing = append(*missing, named)
			}
		}
	}
}

// message is a message of the pinned spec, by its full name.
func message(name protoreflect.FullName) (protoreflect.MessageDescriptor, error) {
	files, refused := parity.Descriptors("a2a")
	if refused != nil {
		return nil, errors.New(refused.GetSays())
	}
	for _, file := range files {
		if found := file.Messages().ByName(name.Name()); found != nil && found.FullName() == name {
			return found, nil
		}
	}
	return nil, errors.Newf("the pinned a2a declares no %s", name)
}
