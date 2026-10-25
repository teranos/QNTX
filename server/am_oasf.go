package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// oasfRecord is OASF's Record, the message the node's record is held to.
const oasfRecord = "agntcy.oasf.types.v1.Record"

// The modules a record carries what the node already says of itself in: the
// Agent Card the asker is given, and the MCP server the asker is shown.
const (
	oasfA2AModule = "integration/a2a"
	oasfMCPModule = "integration/mcp"
)

// amOASFSigil is am oasf: the node as an OASF Record, built from what am node
// answers, the MCP tools, and what each signum names of OASF's skills.
func amOASFSigil() *protocol.Sigil {
	return &protocol.Sigil{
		Name: "oasf",
		Does: "What the node says of itself as an OASF Record, read through the pinned OASF: the skills the signa on the asker's card name, that card and the MCP tools the asker is shown as its modules, and what it leaves empty that OASF requires.",
		Gives: []*protocol.Field{
			{Name: "record", Says: "The record, as the pinned OASF shapes it.", Message: oasfRecord},
			{Name: "missing", Says: "Every attribute OASF requires of a Record that it leaves empty, by its name."},
		},
		Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/am/oasf"},
	}
}

// amOASF is what am oasf answers: the record, and what it lacks.
type amOASF struct {
	Record  json.RawMessage `json:"record"`
	Missing []string        `json:"missing"`
}

func (s *QNTXServer) amOASF(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the record carries the card its caller would be given, and this asking carried no request"}
	}
	record, refused := s.oasfRecordFor(ctx, caller)
	if refused != nil {
		return nil, refused
	}
	body, err := protojson.Marshal(record.Interface())
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrap(err, "the record did not marshal").Error()}
	}
	required, refused := parity.OASFRequired("record")
	if refused != nil {
		return nil, refused
	}
	missing := []string{}
	for _, name := range required {
		if field := record.Descriptor().Fields().ByName(protoreflect.Name(name)); field == nil || leftEmpty(record, field) {
			missing = append(missing, name)
		}
	}
	return amOASF{Record: body, Missing: missing}, nil
}

// leftEmpty is a field the record does not fill: unset, or a list with nothing.
func leftEmpty(m protoreflect.Message, field protoreflect.FieldDescriptor) bool {
	if field.IsList() {
		return m.Get(field).List().Len() == 0
	}
	return !m.Has(field)
}

// oasfRecordFor is the node's record for the caller of r. What the node does
// not have it leaves empty, and nothing is made up to fill what OASF requires.
func (s *QNTXServer) oasfRecordFor(ctx context.Context, r *http.Request) (protoreflect.Message, *protocol.Refusal) {
	failed := func(err error) (protoreflect.Message, *protocol.Refusal) {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	skills, refused := parity.OASFSkills()
	if refused != nil {
		return nil, refused
	}
	modules, refused := parity.OASFModules()
	if refused != nil {
		return nil, refused
	}
	schemaVersion, refused := parity.OASFSchemaVersion()
	if refused != nil {
		return nil, refused
	}

	card := s.a2aCard(r)
	// A skill is named by every signum on the card that names it, once.
	signa := s.checkedSigna()
	named := []map[string]any{}
	var seen []string
	for _, skill := range card.Skills {
		at := slices.IndexFunc(signa, func(signum sigil.Signum) bool { return signum.GetName() == skill.ID })
		if at < 0 {
			continue
		}
		for _, path := range signa[at].GetOasf() {
			id, held := skills[path]
			if !held {
				return failed(errors.Newf("%s names %s, which the pinned OASF holds no skill at", skill.ID, path))
			}
			if slices.Contains(seen, path) {
				continue
			}
			seen = append(seen, path)
			named = append(named, map[string]any{"name": path, "id": id})
		}
	}

	cardMessage, err := card.Message()
	if err != nil {
		return failed(err)
	}
	cardData, err := asJSONObject(cardMessage.Interface())
	if err != nil {
		return failed(errors.Wrap(err, "the card is not a JSON object"))
	}
	tools, err := s.mcpToolsFor(ctx, r)
	if err != nil {
		return failed(err)
	}

	body, err := json.Marshal(map[string]any{
		"name":           appcfg.GetString("node.name"),
		"description":    appcfg.GetString("node.description"),
		"version":        version.VersionTag,
		"schema_version": schemaVersion,
		"skills":         named,
		"modules": []map[string]any{
			{"name": oasfA2AModule, "id": modules[oasfA2AModule], "data": map[string]any{
				"card_data":           cardData,
				"card_schema_version": a2a.Version,
			}},
			{"name": oasfMCPModule, "id": modules[oasfMCPModule], "data": map[string]any{
				"name":        "qntx",
				"tools":       tools,
				"connections": []map[string]any{{"type": "streamable-http", "url": nodeURL(r) + "/mcp"}},
			}},
		},
	})
	if err != nil {
		return failed(errors.Wrap(err, "the record did not marshal"))
	}
	descriptor, err := oasfMessage(oasfRecord)
	if err != nil {
		return failed(err)
	}
	record := dynamicpb.NewMessage(descriptor)
	if err := protojson.Unmarshal(body, record); err != nil {
		return failed(errors.Wrapf(err, "the record is not an %s", oasfRecord))
	}
	return record, nil
}

// mcpToolsFor is every tool the node's MCP lists to the caller of r, as OASF's
// mcp module names a tool: its name, its title and what it is for.
func (s *QNTXServer) mcpToolsFor(ctx context.Context, r *http.Request) ([]map[string]any, error) {
	clientSide, serverSide := mcp.NewInMemoryTransports()
	serving, err := s.mcpServerFor(r).Connect(ctx, serverSide, nil)
	if err != nil {
		return nil, errors.Wrap(err, "the node's MCP did not answer")
	}
	defer serving.Close()
	asking, err := mcp.NewClient(&mcp.Implementation{Name: "am oasf", Version: version.VersionTag}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		return nil, errors.Wrap(err, "the node's MCP was not reached")
	}
	defer asking.Close()
	tools := []map[string]any{}
	for tool, err := range asking.Tools(ctx, nil) {
		if err != nil {
			return nil, errors.Wrap(err, "the node's MCP did not list its tools")
		}
		listed := map[string]any{"name": tool.Name, "description": tool.Description}
		if tool.Title != "" {
			listed["title"] = tool.Title
		}
		tools = append(tools, listed)
	}
	return tools, nil
}

// asJSONObject is a message as the JSON object protojson writes it.
func asJSONObject(m protoreflect.ProtoMessage) (map[string]any, error) {
	raw, err := protojson.Marshal(m)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	return object, nil
}

// oasfMessage is a message of the pinned OASF, by its full name.
func oasfMessage(name protoreflect.FullName) (protoreflect.MessageDescriptor, error) {
	files, refused := parity.Descriptors("oasf")
	if refused != nil {
		return nil, errors.New(refused.GetSays())
	}
	for _, file := range files {
		if found := file.Messages().ByName(name.Name()); found != nil && found.FullName() == name {
			return found, nil
		}
	}
	return nil, errors.Newf("the pinned oasf declares no %s", name)
}
