package server

// Node Connectors' Google Cloud row: the node set up at Google Cloud on ROOT's
// yes, as its own workload identity, and whether Google trades its identity token.
// "Given that I am ROOT and my google identity is the ROOT identity"
// "ROOT, who already set up the Google login in that project, connects the node
// to Google Cloud from Node Connectors. From then on everything on the node
// that talks to Google gets an identity the node issued for it."

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// Where Google answers. Vars so a test points them at a stand-in.
var (
	googleResourceManager = "https://cloudresourcemanager.googleapis.com"
	googleServiceUsage    = "https://serviceusage.googleapis.com"
	googleIAM             = "https://iam.googleapis.com"
	googleSTS             = "https://sts.googleapis.com"
	googleCredentials     = "https://iamcredentials.googleapis.com"
	googleRevoke          = "https://oauth2.googleapis.com/revoke"
)

const (
	googleLineSubject   = "GOOGLE"
	googleLinePredicate = "cloud"
	// What the node makes in ROOT's project: its pool, the provider in it that
	// trusts the node's issuer, and the service account its identity acts as.
	googlePool           = "qntx"
	googleProvider       = "node"
	googleServiceAccount = "qntx-node"
	googleCloudScope     = "https://www.googleapis.com/auth/cloud-platform"
	// googleAsking is how long the node waits on Google for the light.
	googleAsking = 15 * time.Second
)

// googleServices are the APIs the federation runs through: "Enable the IAM,
// Resource Manager, Service Account Credentials, and Security Token Service APIs".
var googleServices = []string{"iam.googleapis.com", "cloudresourcemanager.googleapis.com", "iamcredentials.googleapis.com", "sts.googleapis.com"}

// googleCalls is how the node asks Google.
var googleCalls = &http.Client{Timeout: 30 * time.Second}

// googleAsksAgain is how long the node waits before asking whether Google finished.
var googleAsksAgain = time.Second

// googleError is what Google answered when it did not do what it was asked.
type googleError struct {
	status int
	said   string
}

func (e *googleError) Error() string {
	return "Google answered " + http.StatusText(e.status) + ": " + e.said
}

// googleCall asks Google on token, sending body as JSON and reading the answer into out.
func googleCall(ctx context.Context, token, method, at string, body, out any) error {
	var sent io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return errors.Wrapf(err, "what the node asks %s did not encode", at)
		}
		sent = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, at, sent)
	if err != nil {
		return errors.Wrapf(err, "the node could not ask %s", at)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	answered, err := googleCalls.Do(req)
	if err != nil {
		return errors.Wrapf(err, "%s was not reached", at)
	}
	defer answered.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(answered.Body, 1<<20))
	if err != nil {
		return errors.Wrapf(err, "the answer of %s did not read", at)
	}
	if answered.StatusCode/100 != 2 {
		return errors.Wrapf(&googleError{status: answered.StatusCode, said: strings.TrimSpace(string(raw))}, "%s %s", method, at)
	}
	if out == nil {
		return nil
	}
	return errors.Wrapf(json.Unmarshal(raw, out), "the answer of %s is not what the node asked for", at)
}

// alreadyThere is Google saying what was asked for exists: set up before, kept.
func alreadyThere(err error) bool {
	var said *googleError
	return errors.As(err, &said) && said.status == http.StatusConflict
}

// googleOperation is what Google answers a change it finishes on its own time.
type googleOperation struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// waitFor waits until Google finishes op, asking base for it.
func waitFor(ctx context.Context, token, base string, op googleOperation) error {
	for !op.Done {
		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "Google did not finish %s in time", op.Name)
		case <-time.After(googleAsksAgain):
		}
		if err := googleCall(ctx, token, http.MethodGet, base+"/v1/"+op.Name, nil, &op); err != nil {
			return err
		}
	}
	if op.Error != nil {
		return errors.Newf("Google did not finish %s: %s", op.Name, op.Error.Message)
	}
	return nil
}

// googleProject is a project ROOT's account sees.
type googleProject struct {
	ProjectID     string `json:"projectId"`
	ProjectNumber string `json:"projectNumber"`
}

// projectOf is the project the node is set up in: the one named, or the one
// ROOT's account sees when it sees one.
func projectOf(ctx context.Context, token, named string) (googleProject, error) {
	var seen struct {
		Projects []googleProject `json:"projects"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleResourceManager+"/v1/projects?filter="+url.QueryEscape("lifecycleState:ACTIVE"), nil, &seen); err != nil {
		return googleProject{}, err
	}
	var ids []string
	for _, p := range seen.Projects {
		if named != "" && (p.ProjectID == named || p.ProjectNumber == named) {
			return p, nil
		}
		ids = append(ids, p.ProjectID)
	}
	switch {
	case named != "":
		return googleProject{}, errors.Newf("ROOT's Google account sees no project %s, and sees [%s]", named, strings.Join(ids, ", "))
	case len(seen.Projects) == 1:
		return seen.Projects[0], nil
	case len(seen.Projects) == 0:
		return googleProject{}, errors.New("ROOT's Google account sees no project to set the node up in")
	}
	return googleProject{}, errors.Newf("ROOT's Google account sees %d projects: connect again naming one, %s?project=<id>, of [%s]", len(seen.Projects), auth.GoogleCloudPath(), strings.Join(ids, ", "))
}

// googlePoolOf is the node's pool in project, by its full name.
func googlePoolOf(p googleProject) string {
	return "projects/" + p.ProjectNumber + "/locations/global/workloadIdentityPools/" + googlePool
}

// googleAudience is what the node's identity token is for at Google: its provider.
func googleAudience(projectNumber string) string {
	return "//iam.googleapis.com/projects/" + projectNumber + "/locations/global/workloadIdentityPools/" + googlePool + "/providers/" + googleProvider
}

// connectGoogleCloud sets the node up at Google Cloud on ROOT's token: the APIs
// the federation runs through, a pool whose provider trusts the node's issuer,
// a service account the node's DID may act as, and the node's GOOGLE line. ROOT's
// token is let go once the node no longer needs it.
func (s *QNTXServer) connectGoogleCloud(ctx context.Context, token, named, by string) (string, error) {
	defer s.letGoOfGoogle(token)
	i, err := s.theIssuer()
	if err != nil {
		return "", err
	}
	p, err := projectOf(ctx, token, named)
	if err != nil {
		return "", err
	}
	for _, service := range googleServices {
		var op googleOperation
		if err := googleCall(ctx, token, http.MethodPost, googleServiceUsage+"/v1/projects/"+p.ProjectNumber+"/services/"+service+":enable", map[string]any{}, &op); err != nil {
			return "", errors.Wrapf(err, "%s was not enabled in %s", service, p.ProjectID)
		}
		if err := waitFor(ctx, token, googleServiceUsage, op); err != nil {
			return "", err
		}
	}
	pool := googlePoolOf(p)
	if err := googleCreate(ctx, token, googleIAM+"/v1/projects/"+p.ProjectNumber+"/locations/global/workloadIdentityPools?workloadIdentityPoolId="+googlePool,
		map[string]any{"displayName": "QNTX", "description": "The QNTX node at " + i.url}); err != nil && !alreadyThere(err) {
		return "", errors.Wrapf(err, "the pool %s was not made", pool)
	}
	provider := pool + "/providers/" + googleProvider
	err = googleCreate(ctx, token, googleIAM+"/v1/"+pool+"/providers?workloadIdentityPoolProviderId="+googleProvider, map[string]any{
		"displayName":      "QNTX node",
		"oidc":             map[string]any{"issuerUri": i.url},
		"attributeMapping": map[string]string{"google.subject": "assertion.sub"},
	})
	if alreadyThere(err) {
		err = providerTrusts(ctx, token, provider, i.url)
	}
	if err != nil {
		return "", errors.Wrapf(err, "the provider %s was not made", provider)
	}
	account := googleServiceAccount + "@" + p.ProjectID + ".iam.gserviceaccount.com"
	err = googleCall(ctx, token, http.MethodPost, googleIAM+"/v1/projects/"+p.ProjectID+"/serviceAccounts",
		map[string]any{"accountId": googleServiceAccount, "serviceAccount": map[string]string{"displayName": "QNTX node", "description": "The QNTX node " + s.nodeDID.DID}}, nil)
	if err != nil && !alreadyThere(err) {
		return "", errors.Wrapf(err, "the service account %s was not made", account)
	}
	if err := s.nodeActsAs(ctx, token, p, account); err != nil {
		return "", err
	}
	if refused := s.keepGoogleLine(by, googleKept{project: p, pool: pool, provider: provider, account: account}); refused != nil {
		return "", errors.New(refused.Says)
	}
	return "The node is connected to Google Cloud in " + p.ProjectID + ": it is " + account + " there, through " + provider + ".", nil
}

// googleCreate makes what Google finishes on its own time, and keeps what was made before.
func googleCreate(ctx context.Context, token, at string, body any) error {
	var op googleOperation
	if err := googleCall(ctx, token, http.MethodPost, at, body, &op); err != nil {
		return err
	}
	return waitFor(ctx, token, googleIAM, op)
}

// providerTrusts is whether the provider made before trusts the node's issuer.
func providerTrusts(ctx context.Context, token, provider, issuer string) error {
	var made struct {
		OIDC struct {
			IssuerURI string `json:"issuerUri"`
		} `json:"oidc"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleIAM+"/v1/"+provider, nil, &made); err != nil {
		return err
	}
	if made.OIDC.IssuerURI != issuer {
		return errors.Newf("%s trusts %s, not this node's issuer %s", provider, made.OIDC.IssuerURI, issuer)
	}
	return nil
}

// nodeActsAs lets the node's DID act as account: Workload Identity User for its
// principal in the node's pool, added to what the account's policy held.
func (s *QNTXServer) nodeActsAs(ctx context.Context, token string, p googleProject, account string) error {
	const role = "roles/iam.workloadIdentityUser"
	member := "principal://iam.googleapis.com/" + googlePoolOf(p) + "/subject/" + s.nodeDID.DID
	at := googleIAM + "/v1/projects/" + p.ProjectID + "/serviceAccounts/" + account
	var policy map[string]any
	// A service account just made is not at once visible to its policy.
	var err error
	for range 10 {
		if err = googleCall(ctx, token, http.MethodPost, at+":getIamPolicy", map[string]any{}, &policy); err == nil {
			break
		}
		var said *googleError
		if !errors.As(err, &said) || said.status != http.StatusNotFound {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "%s did not appear", account)
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		return err
	}
	bindings, _ := policy["bindings"].([]any)
	for _, b := range bindings {
		binding, _ := b.(map[string]any)
		if binding["role"] != role {
			continue
		}
		members, _ := binding["members"].([]any)
		if slices.Contains(members, any(member)) {
			return nil
		}
		binding["members"] = append(members, member)
		return googleCall(ctx, token, http.MethodPost, at+":setIamPolicy", map[string]any{"policy": policy}, nil)
	}
	policy["bindings"] = append(bindings, map[string]any{"role": role, "members": []string{member}})
	return googleCall(ctx, token, http.MethodPost, at+":setIamPolicy", map[string]any{"policy": policy}, nil)
}

// letGoOfGoogle revokes ROOT's token: the node keeps no credential of ROOT's.
func (s *QNTXServer) letGoOfGoogle(token string) {
	req, err := http.NewRequest(http.MethodPost, googleRevoke+"?token="+url.QueryEscape(token), nil)
	if err == nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var answered *http.Response
		if answered, err = googleCalls.Do(req); err == nil {
			answered.Body.Close()
			if answered.StatusCode != http.StatusOK {
				err = errors.Newf("Google answered %s", answered.Status)
			}
		}
	}
	if err != nil {
		s.logger.Warnw("ROOT's Google token was not revoked; it lapses on its own within the hour", "error", err)
	}
}

// googleKept is the node's newest GOOGLE line.
type googleKept struct {
	project                 googleProject
	pool, provider, account string
	setBy                   string
	at                      time.Time
}

// keepGoogleLine writes the node's GOOGLE line anew.
func (s *QNTXServer) keepGoogleLine(by string, kept googleKept) *protocol.Refusal {
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "no store to keep the node's %s line in", googleLineSubject).Error()}
	}
	id, err := identity.GenerateASUIDWithRetry("AS", googleLineSubject, googleLinePredicate, auth.NamespaceSystem, store.AttestationExists)
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "failed to name the node's %s line", googleLineSubject).Error()}
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{googleLineSubject}, Predicates: []string{googleLinePredicate}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{by}, Timestamp: now, CreatedAt: now, Source: "google",
		Attributes: map[string]any{
			"project_id": kept.project.ProjectID, "project_number": kept.project.ProjectNumber,
			"pool": kept.pool, "provider": kept.provider, "service_account": kept.account,
		},
	}); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "the node's %s line %s was not kept", googleLineSubject, id).Error()}
	}
	return nil
}

// googleLine reads the node's newest GOOGLE line. False is a node not connected yet.
func (s *QNTXServer) googleLine() (googleKept, bool, error) {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return googleKept{}, false, errors.Wrapf(err, "failed to read the node's %s line", googleLineSubject)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{googleLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return googleKept{}, false, errors.Wrapf(err, "failed to read the node's %s line", googleLineSubject)
	}
	var newest *types.As
	for _, as := range found {
		if newest == nil || as.Timestamp.After(newest.Timestamp) {
			newest = as
		}
	}
	if newest == nil {
		return googleKept{}, false, nil
	}
	kept := googleKept{at: newest.Timestamp}
	for name, into := range map[string]*string{
		"project_id": &kept.project.ProjectID, "project_number": &kept.project.ProjectNumber,
		"pool": &kept.pool, "provider": &kept.provider, "service_account": &kept.account,
	} {
		if *into, err = oneText(newest.Attributes, name, googleLineSubject, newest.ID, auth.NamespaceSystem); err != nil {
			return googleKept{}, false, err
		}
	}
	setters, err := named.ItemsOf(googleLineSubject+" line "+newest.ID+", who connected it,", newest.Actors)
	if err != nil {
		return googleKept{}, false, err
	}
	kept.setBy = setters.First()
	return kept, true, nil
}

// googleSignum is the node's own connection to Google Cloud, ROOT's.
func (s *QNTXServer) googleSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "google",
			Description: "The node at Google Cloud: set up as its own workload identity on ROOT's yes at Google, and whether Google trades its identity token. ROOT's.",
			Tags:        []string{"google", "oidc"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "am",
					Does:   "The node's issuer and subject, where ROOT connects it, what it was set up as at Google Cloud, and whether Google trades its identity token, asked now.",
					Answer: "protocol.NodeGoogle",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/google"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"am": s.googleAm},
	}
}

func (s *QNTXServer) googleAm(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	i, err := s.theIssuer()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am := &protocol.NodeGoogle{Issuer: i.url, Subject: s.nodeDID.DID, Connect: i.url + auth.GoogleCloudPath()}
	kept, held, err := s.googleLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !held {
		return am, nil
	}
	am.ProjectId, am.ProjectNumber, am.Pool, am.Provider, am.ServiceAccount = kept.project.ProjectID, kept.project.ProjectNumber, kept.pool, kept.provider, kept.account
	am.SetBy, am.At = kept.setBy, kept.at.Format(time.RFC3339)
	am.Asked = true
	if _, err := s.googleAccessToken(ctx, i, kept); err != nil {
		am.Why = err.Error()
		return am, nil
	}
	am.Accepted = true
	return am, nil
}

// googleAccessToken is the node at Google, as kept names it: its identity token
// traded at Google's token exchange, and that acting as the node's service account.
func (s *QNTXServer) googleAccessToken(ctx context.Context, i *issuer, kept googleKept) (string, error) {
	asking, cancel := context.WithTimeout(ctx, googleAsking)
	defer cancel()
	audience := googleAudience(kept.project.ProjectNumber)
	subject, err := i.identityTokenFor(s.nodeDID.DID, auth.NamespaceSystem, audience)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"audience":             {audience},
		"scope":                {googleCloudScope},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
		"subject_token":        {subject},
		"subject_token_type":   {"urn:ietf:params:oauth:token-type:jwt"},
	}
	req, err := http.NewRequestWithContext(asking, http.MethodPost, googleSTS+"/v1/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.Wrap(err, "the node could not ask Google's token exchange")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	answered, err := googleCalls.Do(req)
	if err != nil {
		return "", errors.Wrapf(err, "%s was not reached", googleSTS)
	}
	defer answered.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(answered.Body, 1<<16))
	if answered.StatusCode != http.StatusOK {
		return "", errors.Newf("Google's token exchange did not trade the identity of %s at %s: %s", s.nodeDID.DID, audience, strings.TrimSpace(string(raw)))
	}
	var traded struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &traded); err != nil || traded.AccessToken == "" {
		return "", errors.Newf("Google's token exchange answered no token: %s", strings.TrimSpace(string(raw)))
	}
	var acting struct {
		AccessToken string `json:"accessToken"`
	}
	if err := googleCall(asking, traded.AccessToken, http.MethodPost, googleCredentials+"/v1/projects/-/serviceAccounts/"+kept.account+":generateAccessToken",
		map[string]any{"scope": []string{googleCloudScope}}, &acting); err != nil {
		return "", errors.Wrapf(err, "the node's identity may not act as %s", kept.account)
	}
	if acting.AccessToken == "" {
		return "", errors.Newf("Google gave no token for %s", kept.account)
	}
	return acting.AccessToken, nil
}
