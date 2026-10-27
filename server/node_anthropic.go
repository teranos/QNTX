package server

// Node Connectors' Anthropic row (QNTX-Web stories/27-root-registers-the-node-with-anthropic.feature):
// the ids of the rule the node's identity token is traded at, and whether Anthropic accepts it.
// "oidc for the node"

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// The node's line at Anthropic: its subject, and the predicate the rule's ids are kept under.
const (
	anthropicLineSubject   = "ANTHROPIC"
	anthropicLinePredicate = "federation"
	anthropicAPI           = "https://api.anthropic.com"
	// anthropicAsking is how long the node waits on Anthropic for the light.
	anthropicAsking = 15 * time.Second
)

// anthropicIDs are the ids ROOT makes at Anthropic, by the name each is kept under.
var anthropicIDs = []string{"organization_id", "issuer_id", "service_account_id", "federation_rule_id", "workspace_id"}

// anthropicKindOf is the id a word Anthropic shows is, by its prefix or, for
// the organization, by being a UUID: "" is no id of Anthropic's.
func anthropicKindOf(word string) string {
	switch {
	case strings.HasPrefix(word, "fdis_") && len(word) > len("fdis_"):
		return "issuer_id"
	case strings.HasPrefix(word, "svac_") && len(word) > len("svac_"):
		return "service_account_id"
	case strings.HasPrefix(word, "fdrl_") && len(word) > len("fdrl_"):
		return "federation_rule_id"
	case strings.HasPrefix(word, "wrkspc_") && len(word) > len("wrkspc_"):
		return "workspace_id"
	case isUUID(word):
		return "organization_id"
	}
	return ""
}

// isUUID is whether word is a UUID as Anthropic shows an organization's id.
func isUUID(word string) bool {
	if len(word) != 36 {
		return false
	}
	for at, c := range word {
		switch at {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}
	return true
}

// anthropicIDsIn is every id of Anthropic's in what was pasted, placed by its
// kind: a label, quotes or spaces around it are what the Console put there.
func anthropicIDsIn(pasted string) map[string]string {
	found := map[string]string{}
	for _, word := range strings.FieldsFunc(pasted, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '"' || r == '\'' || r == '`' || r == ',' || r == ':' || r == ';'
	}) {
		if kind := anthropicKindOf(word); kind != "" {
			found[kind] = word
		}
	}
	return found
}

// anthropicSignum is the node's own connection to Anthropic, ROOT's.
func (s *QNTXServer) anthropicSignum() sigil.Signum {
	at := "/api/anthropic"
	takes := []*protocol.Param{
		{Name: "given", Required: true, Says: "Whatever was copied from the Console: the node places each id in it by its kind, an fdis_ issuer, an svac_ service account, an fdrl_ rule, a UUID organization."},
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "anthropic",
			Description: "The node at Anthropic: its issuer and subject, the rule its identity token is traded at, and whether Anthropic accepts it. ROOT's.",
			Tags:        []string{"anthropic", "oidc"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "am",
					Does:   "The node's issuer and subject, the ids ROOT gave, and whether Anthropic accepts the node's identity, asked now.",
					Answer: "protocol.NodeAnthropic",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				{
					Name: "order",
					Does: "What ROOT's agents spend first, the session or the Console, and whether the other follows once the first is exhausted.",
					Takes: []*protocol.Param{
						{Name: "first", Required: true, OneOf: anthropicSources, Says: "session, the Claude subscription, or console, the node's OIDC at the Console."},
						{Name: "then", OneOf: []string{"true", "false"}, Says: "Whether the other follows once the first is exhausted. Not sent, it does not."},
					},
					Answer: "protocol.NodeAnthropic",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at + "/order"},
				},
				{
					Name:   "set",
					Does:   "Keeps each id of Anthropic's in what was given, placed by its kind, and asks Anthropic whether it accepts the node once the organization, the service account and the rule are known.",
					Takes:  takes,
					Answer: "protocol.NodeAnthropic",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"am":    s.anthropicAm,
			"set":   s.anthropicSet,
			"order": s.anthropicOrder,
		},
	}
}

func (s *QNTXServer) anthropicAm(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	i, err := s.theIssuer()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am := &protocol.NodeAnthropic{Issuer: i.url, Subject: s.nodeDID.DID, First: anthropicSession, InUse: s.anthropicInUse.Load()}
	if am.Session, err = s.sessionWindows(ctx); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	kept, held, err := s.anthropicLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !held {
		return am, nil
	}
	am.First, am.Then = kept.first, kept.then
	am.OrganizationId, am.IssuerId, am.FederationRuleId, am.ServiceAccountId, am.WorkspaceId = kept.ids["organization_id"], kept.ids["issuer_id"], kept.ids["federation_rule_id"], kept.ids["service_account_id"], kept.ids["workspace_id"]
	am.SetBy, am.At = kept.setBy, kept.at.Format(time.RFC3339)
	// Anthropic is asked once it can be: the organization, the service account and the rule known.
	if am.OrganizationId == "" || am.ServiceAccountId == "" || am.FederationRuleId == "" {
		return am, nil
	}
	am.Asked = true
	if err := s.askAnthropic(ctx, i, am); err != nil {
		am.Why = err.Error()
		return am, nil
	}
	am.Accepted = true
	return am, nil
}

func (s *QNTXServer) anthropicSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the node's ids at Anthropic are given through the gate, and this came through none"}
	}
	found := anthropicIDsIn(sent["given"])
	if len(found) == 0 {
		return nil, &protocol.Refusal{Why: sigil.NotOneOf, Param: "given", Says: "no id of Anthropic's in " + strings.TrimSpace(sent["given"]) + ": an organization's is a UUID, an issuer's starts fdis_, a service account's svac_, a rule's fdrl_"}
	}
	// What was given joins what the node kept: each id is given once, at its own step.
	kept, _, err := s.anthropicLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for name, id := range found {
		kept.ids[name] = id
	}
	if refused := s.keepAnthropicLine(admitted.Identity, kept); refused != nil {
		return nil, refused
	}
	s.logger.Infow("the node's ids at Anthropic were given", "given", found, "by", admitted.Identity)
	return s.anthropicAm(ctx, sent)
}

// keepAnthropicLine writes the node's ANTHROPIC line anew: every id kept, and the order.
func (s *QNTXServer) keepAnthropicLine(by string, kept anthropicKept) *protocol.Refusal {
	attributes := map[string]any{"first": kept.first, "then": kept.then}
	for name, id := range kept.ids {
		if id != "" {
			attributes[name] = id
		}
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "no store to keep the node's %s line in", anthropicLineSubject).Error()}
	}
	id, err := identity.GenerateASUIDWithRetry("AS", anthropicLineSubject, anthropicLinePredicate, auth.NamespaceSystem, store.AttestationExists)
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "failed to name the node's %s line", anthropicLineSubject).Error()}
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{anthropicLineSubject}, Predicates: []string{anthropicLinePredicate}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{by}, Timestamp: now, CreatedAt: now, Source: "anthropic", Attributes: attributes,
	}); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "the node's %s line %s was not kept", anthropicLineSubject, id).Error()}
	}
	return nil
}

// anthropicKept is the node's newest ANTHROPIC line.
type anthropicKept struct {
	ids map[string]string
	// What ROOT's agents spend first, and whether the other follows.
	first string
	then  bool
	setBy string
	at    time.Time
}

// anthropicLine reads the node's newest ANTHROPIC line. False is a node ROOT gave no ids yet.
func (s *QNTXServer) anthropicLine() (anthropicKept, bool, error) {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return anthropicKept{}, false, errors.Wrapf(err, "failed to read the node's %s line", anthropicLineSubject)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{anthropicLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return anthropicKept{}, false, errors.Wrapf(err, "failed to read the node's %s line", anthropicLineSubject)
	}
	var newest *types.As
	for _, as := range found {
		if newest == nil || as.Timestamp.After(newest.Timestamp) {
			newest = as
		}
	}
	if newest == nil {
		return anthropicKept{ids: map[string]string{}, first: anthropicSession}, false, nil
	}
	kept := anthropicKept{ids: map[string]string{}, first: anthropicSession, at: newest.Timestamp}
	// A line kept before ROOT ordered anything spends the session first, and nothing follows.
	if _, ordered := newest.Attributes["first"]; ordered {
		if kept.first, err = oneText(newest.Attributes, "first", anthropicLineSubject, newest.ID, auth.NamespaceSystem); err != nil {
			return anthropicKept{}, false, err
		}
		then, isBool := newest.Attributes["then"].(bool)
		if !isBool {
			return anthropicKept{}, false, errors.Newf("the node's %s line %s keeps then as %T, not a bool", anthropicLineSubject, newest.ID, newest.Attributes["then"])
		}
		kept.then = then
	}
	for _, name := range anthropicIDs {
		// An id not given yet is absent from the line.
		if _, given := newest.Attributes[name]; !given {
			continue
		}
		if kept.ids[name], err = oneText(newest.Attributes, name, anthropicLineSubject, newest.ID, auth.NamespaceSystem); err != nil {
			return anthropicKept{}, false, err
		}
	}
	setters, err := named.ItemsOf(anthropicLineSubject+" line "+newest.ID+", who gave it,", newest.Actors)
	if err != nil {
		return anthropicKept{}, false, err
	}
	kept.setBy = setters.First()
	return kept, true, nil
}

// askAnthropic trades a fresh identity token for the node's DID at the rule
// am names, and lists Anthropic's models on what it was traded for.
func (s *QNTXServer) askAnthropic(ctx context.Context, i *issuer, am *protocol.NodeAnthropic) error {
	base := s.anthropicBaseURL
	if base == "" {
		base = anthropicAPI
	}
	client := anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(base),
		option.WithFederationTokenProvider(i.identityTokenOf(am.Subject, auth.NamespaceSystem), option.FederationOptions{
			FederationRuleID: am.FederationRuleId, OrganizationID: am.OrganizationId, ServiceAccountID: am.ServiceAccountId, WorkspaceID: am.WorkspaceId,
		}),
	)
	asking, cancel := context.WithTimeout(ctx, anthropicAsking)
	defer cancel()
	_, err := client.Models.List(asking, anthropic.ModelListParams{})
	return errors.Wrapf(err, "Anthropic at %s did not accept the identity of %s at rule %s", base, am.Subject, am.FederationRuleId)
}
