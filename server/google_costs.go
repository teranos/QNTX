package server

// The node's costs at Google Cloud: its budgets' own reports, read as its own
// identity, and everything that costs halted at 80% of the lower of Google's
// budget and the limit ROOT sets here.
// "i would be most concerned about costs and hitting limits i put in there and having QNTX be aware of it"
// "it keeps using google, but everything that costs anything needs to be halted until the budget frees up"

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// Where Google answers. Vars so a test points them at a stand-in.
var (
	googleBilling = "https://cloudbilling.googleapis.com"
	googleBudgets = "https://billingbudgets.googleapis.com"
	googlePubSub  = "https://pubsub.googleapis.com"
)

const (
	// googleCostsTopic names the topic and the node's pull subscription to it.
	googleCostsTopic = "qntx-budget"
	// googleHaltsAt is the share of the budget at which what costs halts.
	googleHaltsAt = 0.8
	// googleCostsEvery is how often the node reads what its budgets reported.
	googleCostsEvery = 5 * time.Minute

	googleCostsLineSubject  = "GOOGLE-COSTS"
	googleBudgetLineSubject = "GOOGLE-BUDGET"
	googleLimitLineSubject  = "GOOGLE-LIMIT"

	googleBudgetsPage = "https://console.cloud.google.com/billing/budgets"
	googleBillingPage = "https://console.cloud.google.com/billing/linkedaccount"
)

// googleCostServices are the APIs the costs run through.
var googleCostServices = []string{"cloudbilling.googleapis.com", "billingbudgets.googleapis.com", "pubsub.googleapis.com"}

// grantOn adds member to role in the policy of resource at, keeping what it held.
// read is how that resource's policy is read: Pub/Sub answers a GET, the others a POST.
func grantOn(ctx context.Context, token, at, read, role, member string) error {
	var policy map[string]any
	var body any
	if read == http.MethodPost {
		body = map[string]any{}
	}
	if err := googleCall(ctx, token, read, at+":getIamPolicy", body, &policy); err != nil {
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

// connectGoogleCosts wires the node to its budgets on ROOT's token: it may read the
// billing account, its budgets report to a topic of its own, and it pulls from that.
// ROOT's token is let go once the node no longer needs it.
func (s *QNTXServer) connectGoogleCosts(ctx context.Context, token, by string) (said string, err error) {
	defer s.letGoOfGoogle(token)
	defer func() { err = googleFixOf(err) }()
	kept, held, err := s.googleLine()
	if err != nil {
		return "", err
	}
	if !held {
		return "", &auth.GoogleCloudFix{Says: "Connect this node to Google Cloud first, in Node Connectors.", Full: "the node has no GOOGLE line"}
	}
	project := kept.project.ProjectID
	if err := enableServices(ctx, token, project, googleCostServices); err != nil {
		return "", err
	}
	var billing struct {
		Account string `json:"billingAccountName"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleBilling+"/v1/projects/"+url.PathEscape(project)+"/billingInfo", nil, &billing); err != nil {
		return "", errors.Wrapf(err, "the billing account of %s was not read", project)
	}
	if billing.Account == "" {
		return "", &auth.GoogleCloudFix{Says: project + " has no billing account linked, so there are no costs to watch. Link one, then try again.", Link: googleBillingPage, Full: "billingInfo names no billingAccountName"}
	}
	member := "serviceAccount:" + kept.account
	if err := grantOn(ctx, token, googleBilling+"/v1/"+billing.Account, http.MethodPost, "roles/billing.viewer", member); err != nil {
		return "", errors.Wrapf(err, "%s was not let read %s", kept.account, billing.Account)
	}
	topic := "projects/" + project + "/topics/" + googleCostsTopic
	subscription := "projects/" + project + "/subscriptions/" + googleCostsTopic
	if err := googleCall(ctx, token, http.MethodPut, googlePubSub+"/v1/"+topic, map[string]any{}, nil); err != nil && !alreadyThere(err) {
		return "", errors.Wrapf(err, "the topic %s was not made", topic)
	}
	if err := googleCall(ctx, token, http.MethodPut, googlePubSub+"/v1/"+subscription, map[string]any{"topic": topic}, nil); err != nil && !alreadyThere(err) {
		return "", errors.Wrapf(err, "the subscription %s was not made", subscription)
	}
	if err := grantOn(ctx, token, googlePubSub+"/v1/"+subscription, http.MethodGet, "roles/pubsub.subscriber", member); err != nil {
		return "", errors.Wrapf(err, "%s was not let pull %s", kept.account, subscription)
	}
	var listed struct {
		Budgets []struct {
			Name string `json:"name"`
		} `json:"budgets"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleBudgets+"/v1/"+billing.Account+"/budgets", nil, &listed); err != nil {
		return "", errors.Wrapf(err, "the budgets of %s were not read", billing.Account)
	}
	if len(listed.Budgets) == 0 {
		return "", &auth.GoogleCloudFix{Says: billing.Account + " has no budget, so nothing reports what it costs. Make one, then try again.", Link: googleBudgetsPage, Full: "the Budget API lists none"}
	}
	for _, budget := range listed.Budgets {
		rule := map[string]any{"notificationsRule": map[string]any{"pubsubTopic": topic, "schemaVersion": "1.0"}}
		at := googleBudgets + "/v1/" + budget.Name + "?updateMask=" + url.QueryEscape("notificationsRule.pubsubTopic,notificationsRule.schemaVersion")
		if err := googleCall(ctx, token, http.MethodPatch, at, rule, nil); err != nil {
			return "", errors.Wrapf(err, "%s was not told to report to %s", budget.Name, topic)
		}
	}
	if refused := s.keepGoogleCostLine(googleCostsLineSubject, "wired", by, map[string]any{"billing_account": billing.Account}); refused != nil {
		return "", errors.New(refused.Says)
	}
	return fmt.Sprintf("The node reads the costs of %s, from %d budget(s). Google's first report can take several hours. What costs halts at %d%% of the budget, and you get a mail when it does.", billing.Account, len(listed.Budgets), int(googleHaltsAt*100)), nil
}

// keepGoogleCostLine writes one of the node's lines about its costs.
func (s *QNTXServer) keepGoogleCostLine(subject, predicate, by string, attributes map[string]any) *protocol.Refusal {
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "no store to keep the node's %s line in", subject).Error()}
	}
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, auth.NamespaceSystem, store.AttestationExists)
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "failed to name the node's %s line", subject).Error()}
	}
	now := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{subject}, Predicates: []string{predicate}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{by}, Timestamp: now, CreatedAt: now, Source: "google", Attributes: attributes,
	}); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "the node's %s line %s was not kept", subject, id).Error()}
	}
	return nil
}

// googleCostLines reads every line of subject, newest first.
func (s *QNTXServer) googleCostLines(subject string) ([]*types.As, error) {
	reading, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the node's %s lines", subject)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{subject}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the node's %s lines", subject)
	}
	slices.SortStableFunc(found, func(a, b *types.As) int { return b.Timestamp.Compare(a.Timestamp) })
	return found, nil
}

// googleCostsWired is whether ROOT connected the node's costs.
func (s *QNTXServer) googleCostsWired() (bool, error) {
	lines, err := s.googleCostLines(googleCostsLineSubject)
	return len(lines) > 0, err
}

// googleConnectedStill is whether the node is connected to Google Cloud, which a halt does not touch.
func (s *QNTXServer) googleConnectedStill() bool {
	_, held, err := s.googleLine()
	return err == nil && held
}

// googleBudgetReport is what a budget last reported.
type googleBudgetReport struct {
	id, name, currency, since string
	cost, budget              float64
	published                 time.Time
}

// googleBudgetReports is what each budget last reported, by when Google published it,
// so a report arriving late does not stand over a newer one.
func (s *QNTXServer) googleBudgetReports() ([]googleBudgetReport, error) {
	lines, err := s.googleCostLines(googleBudgetLineSubject)
	if err != nil {
		return nil, err
	}
	newest := map[string]googleBudgetReport{}
	var order []string
	for _, line := range lines {
		text := func(name string) string { held, _ := line.Attributes[name].(string); return held }
		number := func(name string) float64 { held, _ := line.Attributes[name].(float64); return held }
		published, err := time.Parse(time.RFC3339Nano, text("published"))
		if err != nil {
			return nil, errors.Wrapf(err, "%s line %s holds no time it was published", googleBudgetLineSubject, line.ID)
		}
		report := googleBudgetReport{id: text("budget_id"), name: text("name"), currency: text("currency"), since: text("since"), cost: number("cost"), budget: number("budget"), published: published}
		held, seen := newest[report.id]
		if !seen {
			order = append(order, report.id)
		}
		if !seen || report.published.After(held.published) {
			newest[report.id] = report
		}
	}
	slices.Sort(order)
	reports := make([]googleBudgetReport, 0, len(order))
	for _, id := range order {
		reports = append(reports, newest[id])
	}
	return reports, nil
}

// googleLimit is the limit ROOT set on this node; zero is none.
func (s *QNTXServer) googleLimit() (float64, error) {
	lines, err := s.googleCostLines(googleLimitLineSubject)
	if err != nil || len(lines) == 0 {
		return 0, err
	}
	amount, _ := lines[0].Attributes["amount"].(float64)
	return amount, nil
}

// googleSpendingHalted is whether what costs at Google is halted, and which budget
// says so: 80% of the lower of its amount and ROOT's limit here. What it cannot read
// counts as halted, for a node that does not know what it spends does not spend.
func (s *QNTXServer) googleSpendingHalted() (bool, string) {
	wired, err := s.googleCostsWired()
	if err != nil {
		return true, err.Error()
	}
	if !wired {
		return false, ""
	}
	reports, err := s.googleBudgetReports()
	if err != nil {
		return true, err.Error()
	}
	limit, err := s.googleLimit()
	if err != nil {
		return true, err.Error()
	}
	for _, report := range reports {
		against := report.budget
		if limit > 0 && limit < against {
			against = limit
		}
		if against > 0 && report.cost >= against*googleHaltsAt {
			return true, fmt.Sprintf("%s has reached %.2f %s of %.2f, and what costs halts at %d%% of it", report.name, report.cost, report.currency, against, int(googleHaltsAt*100))
		}
	}
	return false, ""
}

// googleHaltChanged mails ROOT when the halt turned on or off, once each way.
func (s *QNTXServer) googleHaltChanged() {
	halted, why := s.googleSpendingHalted()
	s.googleHaltMu.Lock()
	changed := halted != s.googleHalted
	s.googleHalted = halted
	s.googleHaltMu.Unlock()
	if !changed {
		return
	}
	if halted {
		s.tellGoogleOutcome(googleOutcomeMail("What costs at Google Cloud is halted", why+".\n\nThe node stays connected. It resumes by itself when the budget frees up.\n"))
		return
	}
	s.tellGoogleOutcome(googleOutcomeMail("What costs at Google Cloud has resumed", "The budget freed up.\n"))
}

// googleCostsTick reads what the budgets reported since the last time, keeps it,
// and only then acknowledges it, so a report is lost to neither failure.
func (s *QNTXServer) googleCostsTick(ctx context.Context) error {
	wired, err := s.googleCostsWired()
	if err != nil || !wired {
		return err
	}
	kept, held, err := s.googleLine()
	if err != nil || !held {
		return err
	}
	i, err := s.theIssuer()
	if err != nil {
		return err
	}
	token, err := s.googleAccessToken(ctx, i, kept)
	if err != nil {
		return err
	}
	subscription := googlePubSub + "/v1/projects/" + kept.project.ProjectID + "/subscriptions/" + googleCostsTopic
	var pulled struct {
		Received []struct {
			AckID   string `json:"ackId"`
			Message struct {
				Data        string            `json:"data"`
				PublishTime string            `json:"publishTime"`
				Attributes  map[string]string `json:"attributes"`
			} `json:"message"`
		} `json:"receivedMessages"`
	}
	if err := googleCall(ctx, token, http.MethodPost, subscription+":pull", map[string]any{"maxMessages": 100}, &pulled); err != nil {
		return errors.Wrapf(err, "%s was not pulled", subscription)
	}
	var acks []string
	for _, received := range pulled.Received {
		raw, err := base64.StdEncoding.DecodeString(received.Message.Data)
		if err != nil {
			return errors.Wrapf(err, "a budget's report on %s was not base64", subscription)
		}
		var report struct {
			Name     string  `json:"budgetDisplayName"`
			Cost     float64 `json:"costAmount"`
			Budget   float64 `json:"budgetAmount"`
			Currency string  `json:"currencyCode"`
			Since    string  `json:"costIntervalStart"`
		}
		if err := json.Unmarshal(raw, &report); err != nil {
			return errors.Wrapf(err, "a budget's report on %s was not readable", subscription)
		}
		if refused := s.keepGoogleCostLine(googleBudgetLineSubject, "reported", "google", map[string]any{
			"budget_id": received.Message.Attributes["budgetId"], "name": report.Name, "cost": report.Cost, "budget": report.Budget,
			"currency": report.Currency, "since": report.Since, "published": received.Message.PublishTime,
		}); refused != nil {
			return errors.New(refused.Says)
		}
		acks = append(acks, received.AckID)
	}
	if len(acks) > 0 {
		if err := googleCall(ctx, token, http.MethodPost, subscription+":acknowledge", map[string]any{"ackIds": acks}, nil); err != nil {
			return errors.Wrapf(err, "%d report(s) on %s were not acknowledged", len(acks), subscription)
		}
	}
	s.googleHaltChanged()
	return nil
}

// watchGoogleCosts reads the budgets' reports until the node stops.
func (s *QNTXServer) watchGoogleCosts() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(googleCostsEvery):
		}
		ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
		if err := s.googleCostsTick(ctx); err != nil {
			s.logger.Errorw("What the node's budgets at Google reported was not read", "error", err)
		}
		cancel()
	}
}

// googleCostsAm fills in what the node knows of its costs.
func (s *QNTXServer) googleCostsAm(am *protocol.NodeGoogle, connect string) *protocol.Refusal {
	am.CostsConnect = connect + "?project=" + url.QueryEscape(am.GetProjectId()) + "&thing=costs"
	wired, err := s.googleCostsWired()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am.CostsWired = wired
	reports, err := s.googleBudgetReports()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, report := range reports {
		am.Budgets = append(am.Budgets, &protocol.GoogleBudget{
			Name: report.name, Cost: report.cost, Budget: report.budget, Currency: report.currency,
			Since: report.since, At: report.published.Format(time.RFC3339),
		})
	}
	if am.Limit, err = s.googleLimit(); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am.Halted, am.HaltedWhy = s.googleSpendingHalted()
	return nil
}

// googleLimitSet sets the limit ROOT holds on this node. Zero takes it off.
func (s *QNTXServer) googleLimitSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(sent["amount"]), 64)
	if err != nil || amount < 0 {
		return nil, &protocol.Refusal{Why: sigil.NotOneOf, Param: "amount", Says: "amount is a number, in the budget's currency, and zero takes the limit off"}
	}
	by := "root"
	if admitted, ok := auth.AdmissionFrom(ctx); ok && admitted.Identity != "" {
		by = admitted.Identity
	}
	if refused := s.keepGoogleCostLine(googleLimitLineSubject, "set", by, map[string]any{"amount": amount}); refused != nil {
		return nil, refused
	}
	s.googleHaltChanged()
	return s.googleAm(ctx, sent)
}
