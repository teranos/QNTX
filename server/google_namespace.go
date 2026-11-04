package server

// "A Namespace Owner logs in with their own oicd for their namespace access to agy, like how Root does for Node"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/agy"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// googleNamespaceProvider is the provider in the owner's project that trusts the
// node's issuer for its namespaces: the namespace a token is for is an attribute
// of it, and a service account lets that one namespace act as it.
const googleNamespaceProvider = "namespaces"

// googleNamespaceServices are what a namespace's agy runs through in its owner's
// project: the federation, and Vertex AI.
var googleNamespaceServices = append(append([]string{}, googleServices...), googleModelService)

// namespaceGoogleAudience is what a namespace's identity token is for at Google.
func namespaceGoogleAudience(projectNumber string) string {
	return "//iam.googleapis.com/projects/" + projectNumber + "/locations/global/workloadIdentityPools/" + googlePool + "/providers/" + googleNamespaceProvider
}

// namespaceGoogleAccount is the service account a namespace acts as in its owner's
// project: named by the namespace, within Google's thirty characters.
func namespaceGoogleAccount(namespace string) string {
	sum := sha256.Sum256([]byte(namespace))
	return "qntx-ns-" + hex.EncodeToString(sum[:])[:12]
}

// namespaceGoogleOwner is who owns namespace, as its definition names them. A
// namespace without an owner has no one to connect it.
func (s *QNTXServer) namespaceGoogleOwner(namespace string) (string, error) {
	known := s.held.Known()
	if known == nil {
		return "", errors.New("this node keeps no namespace definitions, so no namespace has an owner to connect it to Google Cloud")
	}
	listed, err := known.List()
	if err != nil {
		return "", errors.Wrapf(err, "failed to read who owns %s", namespace)
	}
	found, err := namespaces.Named(listed, namespace)
	if err != nil {
		return "", err
	}
	if found.Definition == nil || found.Definition.Owner == "" {
		return "", errors.Newf("%s names no owner to connect it to Google Cloud", namespace)
	}
	return found.Definition.Owner, nil
}

// connectGoogleNamespace sets namespace up at Google Cloud on its owner's token, in
// a project of the owner's: the APIs, the node's pool with a provider for its
// namespaces, a service account only this namespace acts as, Vertex AI for it, and
// the namespace's GOOGLE line. The owner's token is let go once the node no longer
// needs it.
func (s *QNTXServer) connectGoogleNamespace(ctx context.Context, token, named, by, namespace string) (said string, err error) {
	defer s.letGoOfGoogle(token)
	defer func() { err = googleFixOf(err) }()
	if namespace, err = s.serves(namespace); err != nil {
		return "", err
	}
	i, err := s.theIssuer()
	if err != nil {
		return "", err
	}
	if named != "" {
		if err := enableServices(ctx, token, named, googleNamespaceServices); err != nil {
			return "", err
		}
	}
	p, err := projectOf(ctx, token, named)
	if err != nil {
		return "", err
	}
	if named == "" {
		if err := enableServices(ctx, token, p.ProjectNumber, googleNamespaceServices); err != nil {
			return "", err
		}
	}
	pool := googlePoolOf(p)
	if err := googleCreate(ctx, token, googleIAM+"/v1/projects/"+p.ProjectNumber+"/locations/global/workloadIdentityPools?workloadIdentityPoolId="+googlePool,
		map[string]any{"displayName": "QNTX", "description": "The QNTX node at " + i.url}); err != nil && !alreadyThere(err) {
		return "", errors.Wrapf(err, "the pool %s was not made", pool)
	}
	provider := pool + "/providers/" + googleNamespaceProvider
	err = googleCreate(ctx, token, googleIAM+"/v1/"+pool+"/providers?workloadIdentityPoolProviderId="+googleNamespaceProvider, map[string]any{
		"displayName":      "QNTX namespaces",
		"oidc":             map[string]any{"issuerUri": i.url},
		"attributeMapping": map[string]string{"google.subject": "assertion.sub", "attribute.namespace": "assertion.namespace"},
	})
	if alreadyThere(err) {
		err = providerTrusts(ctx, token, provider, i.url)
	}
	if err != nil {
		return "", errors.Wrapf(err, "the provider %s was not made", provider)
	}
	id := namespaceGoogleAccount(namespace)
	account := id + "@" + p.ProjectID + ".iam.gserviceaccount.com"
	err = googleCall(ctx, token, http.MethodPost, googleIAM+"/v1/projects/"+p.ProjectID+"/serviceAccounts",
		map[string]any{"accountId": id, "serviceAccount": map[string]string{"displayName": "QNTX " + namespace, "description": "The namespace " + namespace + " of the QNTX node " + s.nodeDID.DID}}, nil)
	if err != nil && !alreadyThere(err) {
		return "", errors.Wrapf(err, "the service account %s was not made", account)
	}
	member := "principalSet://iam.googleapis.com/" + pool + "/attribute.namespace/" + namespace
	if err := actsAs(ctx, token, p, account, member); err != nil {
		return "", err
	}
	at := googleResourceManager + "/v1/projects/" + p.ProjectID
	if err := grantOn(ctx, token, at, http.MethodPost, googleRunsModels, "serviceAccount:"+account); err != nil {
		return "", errors.Wrapf(err, "%s was not granted %s in %s", account, googleRunsModels, p.ProjectID)
	}
	kept := googleKept{project: p, pool: pool, provider: provider, account: account}
	if err := s.keepNamespaceGoogleLine(by, namespace, kept); err != nil {
		return "", err
	}
	return namespace + " is set up in " + p.ProjectID + " as " + account + ". Its agent in agy spends what that project spends. Google can take some minutes before it lets it act.", nil
}

// keepNamespaceGoogleLine writes namespace's GOOGLE line anew, in the namespace's
// own store, as its owner.
func (s *QNTXServer) keepNamespaceGoogleLine(by, namespace string, kept googleKept) error {
	admitted := auth.Admitted(auth.LevelUser, namespace)
	admitted.Identity = by
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return errors.Wrapf(err, "no store to keep the %s line of %s in", googleLineSubject, namespace)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", googleLineSubject, googleLinePredicate, namespace, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", googleLineSubject, namespace)
	}
	now := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{googleLineSubject}, Predicates: []string{googleLinePredicate}, Contexts: []string{namespace},
		Actors: []string{by}, Timestamp: now, CreatedAt: now, Source: "google",
		Attributes: map[string]any{
			"project_id": kept.project.ProjectID, "project_number": kept.project.ProjectNumber,
			"pool": kept.pool, "provider": kept.provider, "service_account": kept.account,
		},
	}), "the %s line %s of %s was not kept", googleLineSubject, id, namespace)
}

// namespaceGoogleLine reads namespace's newest GOOGLE line. False is a namespace
// its owner has not connected.
func (s *QNTXServer) namespaceGoogleLine(namespace string) (googleKept, bool, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return googleKept{}, false, errors.Wrapf(err, "failed to read the %s line of %s", googleLineSubject, namespace)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{googleLineSubject}, Predicates: []string{googleLinePredicate}, Limit: ats.EveryRow})
	if err != nil {
		return googleKept{}, false, errors.Wrapf(err, "failed to read the %s line of %s", googleLineSubject, namespace)
	}
	return keptOfNewest(found, namespace)
}

// namespaceGoogleConnect is where namespace's owner connects it to Google Cloud.
func namespaceGoogleConnect(namespace string) string {
	return auth.GoogleCloudPath() + "?" + url.Values{"thing": {"agy"}, "namespace": {namespace}}.Encode()
}

// agyAsTheNamespace is agy signed in as namespace at its owner's Google Cloud: an
// external_account file in the agent's home, and the identity token it reads,
// fresh for the turn.
func (s *QNTXServer) agyAsTheNamespace(agent *rootAgent) (*agy.Google, *protocol.Refusal) {
	kept, held, err := s.namespaceGoogleLine(agent.namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !held {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Says: "the agent of " + agent.namespace + " runs in agy at Google Cloud, and its owner has not connected it: " + namespaceGoogleConnect(agent.namespace)}
	}
	return s.agyAt(agent, kept, namespaceGoogleAudience(kept.project.ProjectNumber), func() error { return s.writeNamespaceAgyToken(agent, kept) })
}

// writeNamespaceAgyToken writes a fresh identity token for agent in its namespace,
// for the provider in the owner's project.
func (s *QNTXServer) writeNamespaceAgyToken(agent *rootAgent, kept googleKept) error {
	i, err := s.theIssuer()
	if err != nil {
		return err
	}
	token, err := i.identityTokenFor(agent.did, agent.namespace, namespaceGoogleAudience(kept.project.ProjectNumber))
	if err != nil {
		return err
	}
	return errors.Wrapf(os.WriteFile(agyTokenFile(agent), []byte(token), 0o600), "the identity token was not written to %s", agyTokenFile(agent))
}

// standsInANamespace is whether agent is a namespace's rather than the Node Agent.
func standsInANamespace(agent *rootAgent) bool {
	return strings.TrimSpace(agent.namespace) != "" && agent.namespace != auth.NamespaceSystem
}

// startAgyRenewal keeps agent's identity token fresh, once per token file.
func (s *QNTXServer) startAgyRenewal(agent *rootAgent) {
	if _, renewing := s.identityTokensRenewed.LoadOrStore(agyTokenFile(agent), true); !renewing {
		sacred.Go("agy.identity-token", func() { s.keepAgyTokenFresh(agent) })
	}
}
