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
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sacred"
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
	// "i want a max 1 per day as well"
	googleDayLimitLineSubject = "GOOGLE-DAY-LIMIT"

	googleBillingPages = "https://console.cloud.google.com/billing"
	googleBillingPage  = "https://console.cloud.google.com/billing/linkedaccount"
)

// googleCostServices are the APIs the costs run through.
var googleCostServices = []string{"cloudbilling.googleapis.com", "billingbudgets.googleapis.com", "pubsub.googleapis.com"}

// grantOn adds member to role in the policy of resource at, keeping what it held.
// read is how that resource's policy is read: Cloud Billing and Pub/Sub answer a GET, Resource Manager a POST.
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

// linkTheBilling links the one open billing account ROOT's account sees to the project,
// and says which. None or several is ROOT's to settle, and what to do is said.
func linkTheBilling(ctx context.Context, token, project string) (string, error) {
	var seen struct {
		Accounts []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Open        bool   `json:"open"`
		} `json:"billingAccounts"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleBilling+"/v1/billingAccounts", nil, &seen); err != nil {
		return "", errors.Wrap(err, "the billing accounts ROOT sees were not read")
	}
	var open []string
	var names []string
	for _, account := range seen.Accounts {
		if account.Open {
			open = append(open, account.Name)
			names = append(names, account.DisplayName+" ("+account.Name+")")
		}
	}
	here := googleBillingPage + "?project=" + url.QueryEscape(project)
	switch len(open) {
	case 0:
		return "", &auth.GoogleCloudFix{Says: "The Google account you said yes with has no open billing account, so " + project + " cannot be linked to one. Open one, then try again.", Link: googleBillingPages, Full: "billingAccounts lists " + strconv.Itoa(len(seen.Accounts)) + " account(s), none open"}
	case 1:
		if err := googleCall(ctx, token, http.MethodPut, googleBilling+"/v1/projects/"+url.PathEscape(project)+"/billingInfo", map[string]any{"billingAccountName": open[0]}, nil); err != nil {
			return "", errors.Wrapf(err, "%s was not linked to %s", project, open[0])
		}
		return open[0], nil
	}
	return "", &auth.GoogleCloudFix{Says: "The Google account you said yes with has several open billing accounts: " + strings.Join(names, ", ") + ". Link the one for " + project + ", then try again.", Link: here, Full: "billingAccounts lists " + strings.Join(open, ", ")}
}

// proveGoogleCosts asks until Google lets the node read what its budgets report, as
// proveGoogle does for acting as its account, and mails ROOT how it ended.
func (s *QNTXServer) proveGoogleCosts() {
	proving, cancel := context.WithTimeout(context.Background(), googleProving)
	defer cancel()
	var said error
	for {
		err := s.googleCostsTick(proving)
		if err == nil {
			s.logger.Infow("Google lets the node read its costs")
			s.tellGoogleOutcome(googleOutcomeMail("The node reads what Google Cloud costs", "Google lets the node read what its budgets report. Google's first report can take several hours.\n"))
			return
		}
		if said == nil || proving.Err() == nil {
			said = err
		}
		select {
		case <-proving.Done():
			s.logger.Errorw("Google did not let the node read its costs", "error", said)
			s.tellGoogleOutcome(googleOutcomeMail("The node does not read what Google Cloud costs", "Google did not let the node read what its budgets report.\n\nPress Connect costs again, and this is what Google said:\n"+said.Error()+"\n"))
			return
		case <-time.After(googleAsksAgain):
		}
	}
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
		// "why does it demand the user to know by heart"
		if billing.Account, err = linkTheBilling(ctx, token, project); err != nil {
			return "", err
		}
	}
	if billing.Account == "" {
		return "", &auth.GoogleCloudFix{Says: project + " has no billing account linked, so there are no costs to watch. Link one, then try again.", Link: googleBillingPage + "?project=" + url.QueryEscape(project), Full: "billingInfo names no billingAccountName"}
	}
	member := "serviceAccount:" + kept.account
	if err := grantOn(ctx, token, googleBilling+"/v1/"+billing.Account, http.MethodGet, "roles/billing.viewer", member); err != nil {
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
		return "", &auth.GoogleCloudFix{Says: billing.Account + " has no budget, so nothing reports what it costs. Make one, then try again.", Link: googleBillingPages + "/" + strings.TrimPrefix(billing.Account, "billingAccounts/") + "/budgets", Full: "the Budget API lists none"}
	}
	for _, budget := range listed.Budgets {
		rule := map[string]any{"notificationsRule": map[string]any{"pubsubTopic": topic, "schemaVersion": "1.0"}}
		at := googleBudgets + "/v1/" + budget.Name + "?updateMask=" + url.QueryEscape("notificationsRule.pubsubTopic,notificationsRule.schemaVersion")
		if err := googleCall(ctx, token, http.MethodPatch, at, rule, nil); err != nil {
			return "", errors.Wrapf(err, "%s was not told to report to %s", budget.Name, topic)
		}
	}
	// "one project per Google spend item (each API key, the OIDC identity), with each budget halting only its own item."
	if err := s.meterTheNode(ctx, token, billing.Account, topic, by, kept); err != nil {
		return "", err
	}
	if refused := s.keepGoogleCostLine(googleCostsLineSubject, "wired", by, map[string]any{"billing_account": billing.Account}); refused != nil {
		return "", errors.New(refused.Says)
	}
	sacred.Go("google.costs.proving", s.proveGoogleCosts)
	return fmt.Sprintf("You get a mail once Google lets the node read them. The node reads the costs of %s, from %d budget(s). Google's first report can take several hours. What costs halts at %d%% of the budget, and you get a mail when it does.", billing.Account, len(listed.Budgets), int(googleHaltsAt*100)), nil
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

// googleBudgetSet is a budget as it is set at Google.
type googleBudgetSet struct {
	// 「[Opus 5.5] id is what its reports carry; projects is its scope, none being the whole account.」
	id, name, currency, period string
	projects                   []string
	amount                     float64
}

// readGoogleBudgets reads the budgets as they are set at Google: what they are in, and
// how often they start over, which is known before Google first reports.
func (s *QNTXServer) readGoogleBudgets(ctx context.Context, token string) error {
	lines, err := s.googleCostLines(googleCostsLineSubject)
	if err != nil || len(lines) == 0 {
		return err
	}
	account, _ := lines[0].Attributes["billing_account"].(string)
	if account == "" {
		return errors.Newf("%s line %s names no billing account", googleCostsLineSubject, lines[0].ID)
	}
	var listed struct {
		Budgets []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Amount      struct {
				Specified struct {
					Currency string `json:"currencyCode"`
					Units    string `json:"units"`
					Nanos    int64  `json:"nanos"`
				} `json:"specifiedAmount"`
			} `json:"amount"`
			Filter struct {
				Period   string   `json:"calendarPeriod"`
				Custom   any      `json:"customPeriod"`
				Projects []string `json:"projects"`
			} `json:"budgetFilter"`
		} `json:"budgets"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleBudgets+"/v1/"+account+"/budgets", nil, &listed); err != nil {
		return errors.Wrapf(err, "the budgets of %s were not read", account)
	}
	set := make([]googleBudgetSet, 0, len(listed.Budgets))
	for _, budget := range listed.Budgets {
		units, _ := strconv.ParseFloat(budget.Amount.Specified.Units, 64)
		period := budget.Filter.Period
		if period == "" && budget.Filter.Custom != nil {
			period = "CUSTOM"
		}
		set = append(set, googleBudgetSet{
			id: budget.Name[strings.LastIndex(budget.Name, "/")+1:], projects: budget.Filter.Projects,
			name: budget.DisplayName, currency: budget.Amount.Specified.Currency, period: period,
			amount: units + float64(budget.Amount.Specified.Nanos)/1e9,
		})
	}
	s.googleHaltMu.Lock()
	s.googleBudgetsSet = set
	s.googleHaltMu.Unlock()
	return nil
}

// googleNow is the time a day is counted by.
var googleNow = time.Now

// 「[Opus 5.5] googleSpentTodayOn is what the budget of id has spent today, and its newest report:」
// what it last reported less what it reported before today in the same period. Before Google's first
// report of a period that began earlier there is nothing to count from, so today is
// counted from today's first report, and a report comes hours after the spending.
func (s *QNTXServer) googleSpentTodayOn(id string) (spent float64, on googleBudgetReport, err error) {
	reports, err := s.googleEveryBudgetReport()
	if err != nil {
		return 0, on, err
	}
	slices.SortStableFunc(reports, func(a, b googleBudgetReport) int { return a.published.Compare(b.published) })
	var each []googleBudgetReport
	for _, report := range reports {
		if report.id == id {
			each = append(each, report)
		}
	}
	today := googleNow().UTC().Truncate(24 * time.Hour)
	if len(each) > 0 {
		newest := each[len(each)-1]
		on = newest
		if newest.published.Before(today) {
			return 0, on, nil
		}
		base, counted := 0.0, false
		for _, report := range each {
			if report.since != newest.since {
				continue
			}
			if report.published.Before(today) {
				base, counted = report.cost, true
			}
		}
		if !counted {
			began, err := time.Parse(time.RFC3339Nano, newest.since)
			if err != nil || began.Before(today) {
				for _, report := range each {
					if report.since == newest.since {
						base = report.cost
						break
					}
				}
			}
		}
		// In cents: 5.80 less 5 is otherwise just under 0.80.
		spent = math.Round((newest.cost-base)*100) / 100
	}
	return spent, on, nil
}

// googleBudgetReports is what each budget last reported, by when Google published it,
// so a report arriving late does not stand over a newer one.
func (s *QNTXServer) googleBudgetReports() ([]googleBudgetReport, error) {
	every, err := s.googleEveryBudgetReport()
	if err != nil {
		return nil, err
	}
	newest := map[string]googleBudgetReport{}
	var order []string
	for _, report := range every {
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

// googleEveryBudgetReport is every report the node keeps.
func (s *QNTXServer) googleEveryBudgetReport() ([]googleBudgetReport, error) {
	lines, err := s.googleCostLines(googleBudgetLineSubject)
	if err != nil {
		return nil, err
	}
	reports := make([]googleBudgetReport, 0, len(lines))
	for _, line := range lines {
		text := func(name string) string { held, _ := line.Attributes[name].(string); return held }
		number := func(name string) float64 { held, _ := line.Attributes[name].(float64); return held }
		published, err := time.Parse(time.RFC3339Nano, text("published"))
		if err != nil {
			return nil, errors.Wrapf(err, "%s line %s holds no time it was published", googleBudgetLineSubject, line.ID)
		}
		report := googleBudgetReport{id: text("budget_id"), name: text("name"), currency: text("currency"), since: text("since"), cost: number("cost"), budget: number("budget"), published: published}
		reports = append(reports, report)
	}
	return reports, nil
}

// 「[Opus 5.5] googleItemLimit is ROOT's newest limit of subject on item: a period's, or a day's.」
// 「[Opus 5.5] A line naming no item was set before items were, on the node. Zero is none.」
func (s *QNTXServer) googleItemLimit(subject, item string) (float64, error) {
	lines, err := s.googleCostLines(subject)
	if err != nil {
		return 0, err
	}
	for _, line := range lines {
		on, _ := line.Attributes["item"].(string)
		if on == "" {
			on = googleNodeItem
		}
		if on == item {
			amount, _ := line.Attributes["amount"].(float64)
			return amount, nil
		}
	}
	return 0, nil
}

// googleHaltChanged mails ROOT when an item's halt turned on or off, once each way, and
// 「[Opus 5.5] names the item: the others run on.」
func (s *QNTXServer) googleHaltChanged() {
	items, err := s.googleSpendItems()
	if err != nil {
		s.logger.Errorw("What spends at Google was not read to say what halted", "error", err)
		return
	}
	for _, item := range items {
		halted, why := s.googleItemHalted(item.name)
		s.googleHaltMu.Lock()
		if s.googleHalted == nil {
			s.googleHalted = map[string]bool{}
		}
		changed := halted != s.googleHalted[item.name]
		s.googleHalted[item.name] = halted
		s.googleHaltMu.Unlock()
		if !changed {
			continue
		}
		if halted {
			s.tellGoogleOutcome(googleOutcomeMail("What "+item.called()+" spends at Google Cloud is halted", why+".\n\nThe node stays connected, and nothing else halts for it. It resumes by itself when the budget frees up.\n"))
			continue
		}
		s.tellGoogleOutcome(googleOutcomeMail("What "+item.called()+" spends at Google Cloud has resumed", "The budget freed up.\n"))
	}
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
	if err := s.readGoogleBudgets(ctx, token); err != nil {
		return err
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
	am.KeyConnect = connect + "?project=" + url.QueryEscape(am.GetProjectId()) + "&thing=key&namespace="
	wired, err := s.googleCostsWired()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am.CostsWired = wired
	am.AgyConnect = connect + "?project=" + url.QueryEscape(am.GetProjectId()) + "&thing=agy"
	if am.AgyWired, err = s.googleAgyWired(); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	reports, err := s.googleBudgetReports()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.googleHaltMu.Lock()
	set := s.googleBudgetsSet
	s.googleHaltMu.Unlock()
	for _, report := range reports {
		budget := &protocol.GoogleBudget{
			Name: report.name, Cost: report.cost, Budget: report.budget, Currency: report.currency,
			Since: report.since, At: report.published.Format(time.RFC3339),
		}
		for _, each := range set {
			if each.name == report.name {
				budget.Period = each.period
			}
		}
		am.Budgets = append(am.Budgets, budget)
	}
	// A budget Google has not reported on yet is known by how it is set.
	for _, each := range set {
		if !slices.ContainsFunc(reports, func(r googleBudgetReport) bool { return r.name == each.name }) {
			am.Budgets = append(am.Budgets, &protocol.GoogleBudget{Name: each.name, Budget: each.amount, Currency: each.currency, Period: each.period})
		}
	}
	items, err := s.googleSpendItems()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, item := range items {
		spending := &protocol.GoogleSpendItem{Name: item.name, Kind: item.kind, ProjectId: item.project.ProjectID, ProjectNumber: item.project.ProjectNumber, Budget: item.budget}
		if item.budget != "" {
			for _, report := range reports {
				if report.id == item.meter() {
					spending.Cost = report.cost
				}
			}
			if spending.SpentToday, _, err = s.googleSpentTodayOn(item.meter()); err != nil {
				return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
			}
		}
		if spending.Limit, err = s.googleItemLimit(googleLimitLineSubject, item.name); err != nil {
			return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		if spending.DayLimit, err = s.googleItemLimit(googleDayLimitLineSubject, item.name); err != nil {
			return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		spending.Halted, spending.HaltedWhy = s.googleItemHalted(item.name)
		am.Items = append(am.Items, spending)
		// 「[Opus 5.5] What the node itself spends stays where it was answered before items were.」
		if item.name == googleNodeItem {
			am.Limit, am.DayLimit, am.SpentToday, am.Halted, am.HaltedWhy = spending.Limit, spending.DayLimit, spending.SpentToday, spending.Halted, spending.HaltedWhy
		}
	}
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
	item := strings.TrimSpace(sent["item"])
	if item == "" {
		item = googleNodeItem
	}
	if _, found, err := s.googleSpendItemNamed(item); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	} else if !found {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "item", Says: "nothing spends at Google as " + item + ": node, or a namespace whose key was made there"}
	}
	subject := googleLimitLineSubject
	switch sent["per"] {
	case "", "period":
	case "day":
		subject = googleDayLimitLineSubject
	default:
		return nil, &protocol.Refusal{Why: sigil.NotOneOf, Param: "per", Says: "per is period or day, and this is " + sent["per"]}
	}
	if refused := s.keepGoogleCostLine(subject, "set", by, map[string]any{"amount": amount, "item": item}); refused != nil {
		return nil, refused
	}
	s.googleHaltChanged()
	return s.googleAm(ctx, sent)
}
