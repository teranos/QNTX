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

// anthropicIDs are the three ids ROOT gives, by the name each is given under.
var anthropicIDs = []string{"organization_id", "federation_rule_id", "service_account_id"}

// anthropicSignum is the node's own connection to Anthropic, ROOT's.
func (s *QNTXServer) anthropicSignum() sigil.Signum {
	at := "/api/anthropic"
	takes := []*protocol.Param{
		{Name: "organization_id", Required: true, Says: "The Anthropic organization the rule is in, from its Organization page."},
		{Name: "federation_rule_id", Required: true, Says: "The federation rule (fdrl_) the node's identity token is traded at."},
		{Name: "service_account_id", Required: true, Says: "The service account (svac_) the traded token acts as."},
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
					Name:   "set",
					Does:   "Keeps the ids of the rule the node's identity token is traded at, then asks Anthropic whether it accepts it.",
					Takes:  takes,
					Answer: "protocol.NodeAnthropic",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"am":  s.anthropicAm,
			"set": s.anthropicSet,
		},
	}
}

func (s *QNTXServer) anthropicAm(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	i, err := s.theIssuer()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am := &protocol.NodeAnthropic{Issuer: i.url, Subject: s.nodeDID.DID}
	kept, held, err := s.anthropicLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !held {
		return am, nil
	}
	am.OrganizationId, am.FederationRuleId, am.ServiceAccountId = kept.ids["organization_id"], kept.ids["federation_rule_id"], kept.ids["service_account_id"]
	am.SetBy, am.At = kept.setBy, kept.at.Format(time.RFC3339)
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
	attributes := map[string]any{}
	for _, name := range anthropicIDs {
		given, err := named.TextOf(name, strings.TrimSpace(sent[name]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: name, Says: err.Error()}
		}
		attributes[name] = given.String()
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "no store to keep the node's %s line in", anthropicLineSubject).Error()}
	}
	id, err := identity.GenerateASUIDWithRetry("AS", anthropicLineSubject, anthropicLinePredicate, auth.NamespaceSystem, store.AttestationExists)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "failed to name the node's %s line", anthropicLineSubject).Error()}
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{anthropicLineSubject}, Predicates: []string{anthropicLinePredicate}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{admitted.Identity}, Timestamp: now, CreatedAt: now, Source: "anthropic", Attributes: attributes,
	}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "the node's %s line %s was not kept", anthropicLineSubject, id).Error()}
	}
	s.logger.Infow("the node's ids at Anthropic were given", "organization_id", attributes["organization_id"], "federation_rule_id", attributes["federation_rule_id"], "service_account_id", attributes["service_account_id"], "by", admitted.Identity)
	return s.anthropicAm(ctx, sent)
}

// anthropicKept is the node's newest ANTHROPIC line.
type anthropicKept struct {
	ids   map[string]string
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
		return anthropicKept{}, false, nil
	}
	kept := anthropicKept{ids: map[string]string{}, at: newest.Timestamp}
	for _, name := range anthropicIDs {
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
			FederationRuleID: am.FederationRuleId, OrganizationID: am.OrganizationId, ServiceAccountID: am.ServiceAccountId,
		}),
	)
	asking, cancel := context.WithTimeout(ctx, anthropicAsking)
	defer cancel()
	_, err := client.Models.List(asking, anthropic.ModelListParams{})
	return errors.Wrapf(err, "Anthropic at %s did not accept the identity of %s at rule %s", base, am.Subject, am.FederationRuleId)
}
