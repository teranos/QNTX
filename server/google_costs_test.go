package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// standInBilling is Cloud Billing, its budgets and Pub/Sub as the node meets them.
type standInBilling struct {
	mu      sync.Mutex
	account string
	budgets []string
	patched map[string]string
	roles   map[string][]string
	topic   bool
	sub     bool
	waiting []map[string]any
	acked   []string
	enabled []string
	// seen is the billing accounts ROOT's account sees, each open unless named closed.
	seen   []string
	closed []string
}

func billingStandingIn(t *testing.T, s *QNTXServer, account string, budgets ...string) *standInBilling {
	t.Helper()
	b := &standInBilling{account: account, budgets: budgets, patched: map[string]string{}, roles: map[string][]string{}}
	served := httptest.NewServer(http.HandlerFunc(b.answer))
	t.Cleanup(served.Close)
	billing, budgeting, pubsub, usage := googleBilling, googleBudgets, googlePubSub, googleServiceUsage
	googleBilling, googleBudgets, googlePubSub, googleServiceUsage = served.URL, served.URL, served.URL, served.URL
	t.Cleanup(func() {
		googleBilling, googleBudgets, googlePubSub, googleServiceUsage = billing, budgeting, pubsub, usage
	})
	return b
}

func (b *standInBilling) answer(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	path := r.URL.Path
	var sent map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&sent)
	}
	switch {
	case strings.Contains(path, "/services/") && strings.HasSuffix(path, ":enable"):
		service := path[strings.Index(path, "/services/")+len("/services/"):]
		b.enabled = append(b.enabled, strings.TrimSuffix(service, ":enable"))
		write(googleOperation{Name: "operations/x", Done: true})
	case path == "/v1/billingAccounts" && r.Method == http.MethodGet:
		listed := []map[string]any{}
		for _, name := range b.seen {
			listed = append(listed, map[string]any{"name": name, "displayName": "the " + name, "open": !slices.Contains(b.closed, name)})
		}
		write(map[string]any{"billingAccounts": listed})
	case path == "/v1/projects/the-project/billingInfo" && r.Method == http.MethodPut:
		b.account = sent["billingAccountName"].(string)
		write(map[string]any{"billingAccountName": b.account})
	case path == "/v1/projects/the-project/billingInfo":
		write(map[string]any{"billingAccountName": b.account, "billingEnabled": b.account != ""})
	case strings.HasSuffix(path, ":getIamPolicy") && r.Method != http.MethodGet:
		// Both Cloud Billing and Pub/Sub, by their discovery documents.
		w.WriteHeader(http.StatusNotFound)
		write(map[string]any{"error": map[string]any{"message": "getIamPolicy is a GET here"}})
	case strings.HasSuffix(path, ":getIamPolicy"):
		write(map[string]any{"etag": "e"})
	case strings.HasSuffix(path, ":setIamPolicy"):
		at := strings.TrimSuffix(path, ":setIamPolicy")
		for _, each := range sent["policy"].(map[string]any)["bindings"].([]any) {
			binding := each.(map[string]any)
			for _, member := range binding["members"].([]any) {
				b.roles[at] = append(b.roles[at], binding["role"].(string)+" "+member.(string))
			}
		}
		write(map[string]any{})
	case path == "/v1/projects/the-project/topics/"+googleCostsTopic && r.Method == http.MethodPut:
		b.topic = true
		write(map[string]any{})
	case path == "/v1/projects/the-project/subscriptions/"+googleCostsTopic && r.Method == http.MethodPut:
		b.sub = sent["topic"] == "projects/the-project/topics/"+googleCostsTopic
		write(map[string]any{})
	case path == "/v1/projects/the-project/subscriptions/"+googleCostsTopic+":pull":
		write(map[string]any{"receivedMessages": b.waiting})
		b.waiting = nil
	case path == "/v1/projects/the-project/subscriptions/"+googleCostsTopic+":acknowledge":
		for _, id := range sent["ackIds"].([]any) {
			b.acked = append(b.acked, id.(string))
		}
		write(map[string]any{})
	case path == "/v1/"+b.account+"/budgets" && r.Method == http.MethodGet:
		listed := []map[string]any{}
		for _, name := range b.budgets {
			listed = append(listed, map[string]any{
				"name": b.account + "/budgets/" + name, "displayName": name,
				"amount":       map[string]any{"specifiedAmount": map[string]any{"currencyCode": "EUR", "units": "20"}},
				"budgetFilter": map[string]any{"calendarPeriod": "MONTH"},
			})
		}
		write(map[string]any{"budgets": listed})
	case strings.HasPrefix(path, "/v1/"+b.account+"/budgets/") && r.Method == http.MethodPatch:
		rule := sent["notificationsRule"].(map[string]any)
		if rule["schemaVersion"] != "1.0" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b.patched[path[strings.LastIndex(path, "/")+1:]] = rule["pubsubTopic"].(string)
		write(map[string]any{})
	default:
		w.WriteHeader(http.StatusNotFound)
		write(map[string]any{"error": map[string]any{"message": "the stand-in has no " + r.Method + " " + path}})
	}
}

// says queues what Cloud Billing publishes of a budget.
func (b *standInBilling) says(id, ack, published, name string, cost, budget float64, since string) {
	data, _ := json.Marshal(map[string]any{"budgetDisplayName": name, "costAmount": cost, "budgetAmount": budget, "currencyCode": "EUR", "costIntervalStart": since})
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waiting = append(b.waiting, map[string]any{"ackId": ack, "message": map[string]any{
		"data": base64.StdEncoding.EncodeToString(data), "publishTime": published, "attributes": map[string]string{"budgetId": id},
	}})
}

// connectedToGoogle is a node ROOT connected, whose Google stand-in lets it act.
func connectedToGoogle(t *testing.T) (*QNTXServer, *standInGoogle) {
	t.Helper()
	s, _, _ := runningNamespaceAgents(t)
	g := googleStandingIn(t, s, theProject)
	// Connecting proves itself in the background, and mails ROOT when it has.
	proved := make(chan services.NodeMail, 8)
	s.googleOutcome = func(m services.NodeMail) { proved <- m }
	_, err := s.connectGoogleCloud(context.Background(), "root-token", "the-project", "google:root-sub")
	require.NoError(t, err)
	select {
	case m := <-proved:
		require.Contains(t, m.Subject, "is connected")
	case <-time.After(10 * time.Second):
		t.Fatal("connecting never said how it ended")
	}
	return s, g
}

// Pressed, the node can read its billing account, reads its budgets' reports from
// a topic of its own, and keeps no credential of ROOT's.
func TestConnectingCostsWiresTheBudgetsToTheNode(t *testing.T) {
	s, g := connectedToGoogle(t)
	b := billingStandingIn(t, s, "billingAccounts/0-1", "monthly")

	said, err := s.connectGoogleCosts(context.Background(), "root-token-2", "google:root-sub")
	require.NoError(t, err)
	assert.Contains(t, said, "hours", "the first report can take that long, and the page says so")

	assert.ElementsMatch(t, googleCostServices, b.enabled)
	assert.Contains(t, b.roles["/v1/billingAccounts/0-1"], "roles/billing.viewer serviceAccount:"+theAccount)
	assert.True(t, b.topic)
	assert.True(t, b.sub)
	assert.Contains(t, b.roles["/v1/projects/the-project/subscriptions/"+googleCostsTopic], "roles/pubsub.subscriber serviceAccount:"+theAccount)
	assert.Equal(t, "projects/the-project/topics/"+googleCostsTopic, b.patched["monthly"])
	g.mu.Lock()
	assert.Contains(t, g.revoked, "root-token-2", "the node keeps no credential of ROOT's")
	g.mu.Unlock()
	am := rootAsksGoogle(t, s)
	assert.True(t, am.GetCostsWired())
	assert.Contains(t, am.GetCostsConnect(), "thing=costs")
}

// "why does it demand the user to know by heart": the node knows its project, and
// the one open billing account ROOT's account sees is linked to it.
func TestTheOneOpenBillingAccountIsLinkedToTheProject(t *testing.T) {
	s, _ := connectedToGoogle(t)
	b := billingStandingIn(t, s, "", "monthly")
	b.seen, b.closed = []string{"billingAccounts/old", "billingAccounts/0-1"}, []string{"billingAccounts/old"}
	_, err := s.connectGoogleCosts(context.Background(), "root-token", "google:root-sub")
	require.NoError(t, err)
	assert.Equal(t, "billingAccounts/0-1", b.account)
	assert.True(t, rootAsksGoogle(t, s).GetCostsWired())
}

// None open is ROOT's to open, and what to do is said.
func TestNoOpenBillingAccountSaysWhatToDo(t *testing.T) {
	s, _ := connectedToGoogle(t)
	b := billingStandingIn(t, s, "")
	b.seen, b.closed = []string{"billingAccounts/old"}, []string{"billingAccounts/old"}
	_, err := s.connectGoogleCosts(context.Background(), "root-token", "google:root-sub")
	var fix *auth.GoogleCloudFix
	require.True(t, errors.As(err, &fix), "%v", err)
	assert.Contains(t, fix.Says, "no open billing account")
	assert.Empty(t, b.account)
}

// Several are named, not guessed.
func TestSeveralBillingAccountsAreNamedNotGuessed(t *testing.T) {
	s, _ := connectedToGoogle(t)
	b := billingStandingIn(t, s, "")
	b.seen = []string{"billingAccounts/a", "billingAccounts/b"}
	_, err := s.connectGoogleCosts(context.Background(), "root-token", "google:root-sub")
	var fix *auth.GoogleCloudFix
	require.True(t, errors.As(err, &fix), "%v", err)
	assert.Contains(t, fix.Says, "billingAccounts/a")
	assert.Contains(t, fix.Says, "billingAccounts/b")
	assert.Contains(t, fix.Link, "project=the-project", "where to do it, in the project it is for")
	assert.Empty(t, b.account)
}

// A billing account with no budget reports nothing, and what to do is said.
func TestCostsWithNoBudgetSayWhatToDo(t *testing.T) {
	s, _ := connectedToGoogle(t)
	billingStandingIn(t, s, "billingAccounts/0-1")
	_, err := s.connectGoogleCosts(context.Background(), "root-token", "google:root-sub")
	var fix *auth.GoogleCloudFix
	require.True(t, errors.As(err, &fix), "%v", err)
	assert.Contains(t, fix.Says, "no budget")
	assert.Contains(t, fix.Link, "/billing/0-1/budgets", "where to do it, for the account it is about")
}

func wiredForCosts(t *testing.T) (*QNTXServer, *standInBilling, chan services.NodeMail) {
	t.Helper()
	s, _ := connectedToGoogle(t)
	b := billingStandingIn(t, s, "billingAccounts/0-1", "monthly")
	proved := make(chan services.NodeMail, 8)
	s.googleOutcome = func(m services.NodeMail) { proved <- m }
	_, err := s.connectGoogleCosts(context.Background(), "root-token-2", "google:root-sub")
	require.NoError(t, err)
	// "why not send a mail when its fine"
	select {
	case m := <-proved:
		require.Contains(t, m.Subject, "reads what Google Cloud costs")
	case <-time.After(10 * time.Second):
		t.Fatal("wiring the costs never said how it ended")
	}
	mailed := make(chan services.NodeMail, 8)
	s.googleOutcome = func(m services.NodeMail) { mailed <- m }
	return s, b, mailed
}

// What a budget reports is what the node knows: acknowledged, and the newest kept
// whatever order it came in.
func TestBudgetReportsAreKeptNewestFirst(t *testing.T) {
	s, b, _ := wiredForCosts(t)
	b.says("b1", "ack-new", "2026-11-02T12:00:00Z", "monthly", 3.5, 20, "2026-11-01T00:00:00Z")
	b.says("b1", "ack-old", "2026-11-02T06:00:00Z", "monthly", 1, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	assert.ElementsMatch(t, []string{"ack-new", "ack-old"}, b.acked)
	am := rootAsksGoogle(t, s)
	require.Len(t, am.GetBudgets(), 1)
	assert.Equal(t, "monthly", am.GetBudgets()[0].GetName())
	assert.InDelta(t, 3.5, am.GetBudgets()[0].GetCost(), 0.001)
	assert.InDelta(t, 20, am.GetBudgets()[0].GetBudget(), 0.001)
	assert.Equal(t, "EUR", am.GetBudgets()[0].GetCurrency())
	assert.False(t, am.GetHalted())
}

// "its also annoying we dont know if its euro or dollars": what a budget is in, and how
// often it starts over, is known before Google first reports.
func TestABudgetIsKnownBeforeGoogleReports(t *testing.T) {
	s, _, _ := wiredForCosts(t)
	am := rootAsksGoogle(t, s)
	require.Len(t, am.GetBudgets(), 1)
	budget := am.GetBudgets()[0]
	assert.Equal(t, "monthly", budget.GetName())
	assert.Equal(t, "EUR", budget.GetCurrency())
	assert.Equal(t, "MONTH", budget.GetPeriod())
	assert.InDelta(t, 20, budget.GetBudget(), 0.001)
	assert.Empty(t, budget.GetAt(), "Google has not reported")
}

// "i want a max 1 per day as well": what was spent today halts at 80% of it, and the
// next day starts over.
func TestADayLimitHaltsUntilTomorrow(t *testing.T) {
	s, b, mailed := wiredForCosts(t)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	googleNow = func() time.Time { return now }
	t.Cleanup(func() { googleNow = time.Now })

	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "google:root-sub"
	asked := httptest.NewRequest(http.MethodPost, "/api/google/limit", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	_, refused := s.googleSignum().Answers["limit"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": "1", "per": "day"})
	require.Nil(t, refused)
	assert.InDelta(t, 1, rootAsksGoogle(t, s).GetDayLimit(), 0.001)

	b.says("b1", "a1", "2026-11-19T23:00:00Z", "monthly", 5, 20, "2026-11-01T00:00:00Z")
	b.says("b1", "a2", "2026-11-20T06:00:00Z", "monthly", 5.5, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	am := rootAsksGoogle(t, s)
	assert.InDelta(t, 0.5, am.GetSpentToday(), 0.001)
	assert.False(t, am.GetHalted(), "5.50 is under the budget's 80%, and 0.50 under the day's")

	b.says("b1", "a3", "2026-11-20T11:00:00Z", "monthly", 5.8, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	am = rootAsksGoogle(t, s)
	assert.True(t, am.GetHalted(), "0.80 is 80%% of the day's 1")
	assert.Contains(t, am.GetHaltedWhy(), "today")
	require.Len(t, mailed, 1)
	<-mailed

	now = now.Add(24 * time.Hour)
	require.NoError(t, s.googleCostsTick(context.Background()))
	assert.False(t, rootAsksGoogle(t, s).GetHalted(), "tomorrow starts over")
	require.Len(t, mailed, 1, "ROOT is told it resumed")

	_, refused = s.googleSignum().Answers["limit"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": "1", "per": "week"})
	require.NotNil(t, refused)
	assert.Equal(t, "per", refused.GetParam())
}

// A QNTX-side limit below Google's budget is what 80% is of.
func TestAQNTXSideLimitHaltsBeforeGooglesBudget(t *testing.T) {
	s, b, mailed := wiredForCosts(t)
	b.says("b1", "a1", "2026-11-20T00:00:00Z", "monthly", 8, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	halted, _ := s.googleSpendingHalted()
	assert.False(t, halted)

	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "google:root-sub"
	asked := httptest.NewRequest(http.MethodPost, "/api/google/limit", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	answer, refused := s.googleSignum().Answers["limit"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": "10"})
	require.Nil(t, refused)
	holds(t, s.googleSignum(), "limit", answer)

	am := rootAsksGoogle(t, s)
	assert.InDelta(t, 10, am.GetLimit(), 0.001)
	assert.True(t, am.GetHalted(), "8 is 80% of the limit of 10")
	require.Len(t, mailed, 1)

	_, refused = s.googleSignum().Answers["limit"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": "ten"})
	require.NotNil(t, refused)
	assert.Equal(t, "amount", refused.GetParam())

	// Zero takes the limit off.
	_, refused = s.googleSignum().Answers["limit"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": "0"})
	require.Nil(t, refused)
	assert.False(t, rootAsksGoogle(t, s).GetHalted())
}

// What costs halts at 80%, the node stays connected, ROOT is told once each way,
// and it resumes when the next period's report frees the budget.
func TestWhatCostsHaltsAt80PercentAndResumesWhenTheBudgetFreesUp(t *testing.T) {
	s, b, mailed := wiredForCosts(t)
	b.says("b1", "a1", "2026-11-20T00:00:00Z", "monthly", 15.99, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	halted, _ := s.googleSpendingHalted()
	assert.False(t, halted, "under 80%")
	assert.Empty(t, mailed)

	b.says("b1", "a2", "2026-11-21T00:00:00Z", "monthly", 16, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	halted, why := s.googleSpendingHalted()
	assert.True(t, halted)
	assert.Contains(t, why, "monthly")
	assert.True(t, rootAsksGoogle(t, s).GetHalted())
	assert.True(t, s.googleConnectedStill(), "the node stays connected: only what costs halts")
	require.Len(t, mailed, 1)
	assert.Contains(t, (<-mailed).Subject, "halted")

	b.says("b1", "a3", "2026-11-22T00:00:00Z", "monthly", 17, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	assert.Empty(t, mailed, "told once, not with every report")

	b.says("b1", "a4", "2026-12-01T06:00:00Z", "monthly", 0.2, 20, "2026-12-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	halted, _ = s.googleSpendingHalted()
	assert.False(t, halted)
	require.Len(t, mailed, 1)
	assert.Contains(t, (<-mailed).Subject, "resumed")
}

// A node not wired pulls nothing and halts nothing.
func TestANodeNotWiredForCostsHaltsNothing(t *testing.T) {
	s, _ := connectedToGoogle(t)
	require.NoError(t, s.googleCostsTick(context.Background()))
	halted, _ := s.googleSpendingHalted()
	assert.False(t, halted)
	assert.False(t, rootAsksGoogle(t, s).GetCostsWired())
}
