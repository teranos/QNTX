package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "daily spend does need to be per spend-item"
// "one project per Google spend item (each API key, the OIDC identity), with each budget halting only its own item."

// standInKeys is Resource Manager making projects, and API Keys minting keys in them.
type standInKeys struct {
	mu       sync.Mutex
	projects map[string]map[string]any
	keys     map[string]map[string]any
	strings  map[string]string
}

func keysStandingIn(t *testing.T) *standInKeys {
	t.Helper()
	k := &standInKeys{projects: map[string]map[string]any{}, keys: map[string]map[string]any{}, strings: map[string]string{}}
	served := httptest.NewServer(http.HandlerFunc(k.answer))
	t.Cleanup(served.Close)
	projects, keys := googleProjects, googleAPIKeys
	googleProjects, googleAPIKeys = served.URL, served.URL
	t.Cleanup(func() { googleProjects, googleAPIKeys = projects, keys })
	return k
}

func (k *standInKeys) answer(w http.ResponseWriter, r *http.Request) {
	k.mu.Lock()
	defer k.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	var sent map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&sent)
	}
	path := r.URL.Path
	switch {
	case path == "/v3/projects" && r.Method == http.MethodPost:
		id := sent["projectId"].(string)
		k.projects[id] = sent
		// Made on Google's next ask, as an operation.
		write(map[string]any{"name": "operations/cp." + id, "done": false})
	case strings.HasPrefix(path, "/v3/operations/cp."):
		write(map[string]any{"name": strings.TrimPrefix(path, "/v3/"), "done": true})
	case strings.HasPrefix(path, "/v3/projects/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "/v3/projects/")
		if k.projects[id] == nil {
			w.WriteHeader(http.StatusNotFound)
			write(map[string]any{"error": map[string]any{"message": "no project " + id}})
			return
		}
		write(map[string]any{"name": "projects/9" + id[len(id)-6:], "projectId": id})
	case strings.HasSuffix(path, "/locations/global/keys") && r.Method == http.MethodPost:
		name := strings.TrimPrefix(path, "/v2/") + "/k" + string(rune('0'+len(k.keys)+1))
		k.keys[name] = sent
		k.strings[name] = "AIza-" + name
		write(map[string]any{"name": "operations/akmf." + name, "done": true, "response": map[string]any{"name": name}})
	case strings.HasSuffix(path, "/keyString") && r.Method == http.MethodGet:
		name := strings.TrimSuffix(strings.TrimPrefix(path, "/v2/"), "/keyString")
		write(map[string]any{"keyString": k.strings[name]})
	default:
		w.WriteHeader(http.StatusNotFound)
		write(map[string]any{"error": map[string]any{"message": "the stand-in has no " + r.Method + " " + path}})
	}
}

// meterOf is the budget the node made for the project numbered number: its id, and as it was asked for.
func meterOf(t *testing.T, b *standInBilling, number string) (string, map[string]any) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range b.order {
		made := b.made[id]
		filter, _ := made["budgetFilter"].(map[string]any)
		projects, _ := filter["projects"].([]any)
		if len(projects) == 1 && projects[0] == "projects/"+number {
			return id, made
		}
	}
	t.Fatalf("no budget was made for projects/%s: %v", number, b.made)
	return "", nil
}

// itemOf is the spend item named in what google am answers.
func itemOf(t *testing.T, am *protocol.NodeGoogle, name string) *protocol.GoogleSpendItem {
	t.Helper()
	for _, item := range am.GetItems() {
		if item.GetName() == name {
			return item
		}
	}
	t.Fatalf("google am names no spend item %s: %v", name, am.GetItems())
	return nil
}

// rootSetsLimit is ROOT setting a limit on the item.
func rootSetsLimit(t *testing.T, s *QNTXServer, sent sigil.Sent) *protocol.Refusal {
	t.Helper()
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "google:root-sub"
	asked := httptest.NewRequest(http.MethodPost, "/api/google/limit", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	_, refused := s.googleSignum().Answers["limit"](sigil.WithCaller(asked.Context(), asked), sent)
	return refused
}

// The node's own identity is one spend item: its project gets a budget of its own,
// scoped to it alone, reporting to the node.
func TestConnectingCostsMetersTheNodeInItsOwnProject(t *testing.T) {
	s, b, _ := wiredForCosts(t)
	id, made := meterOf(t, b, theProject.ProjectNumber)
	assert.Contains(t, made["amount"], "lastPeriodAmount", "a meter, not a cap: ROOT's limits are what halt")
	assert.Equal(t, "projects/the-project/topics/"+googleCostsTopic, made["notificationsRule"].(map[string]any)["pubsubTopic"])

	node := itemOf(t, rootAsksGoogle(t, s), googleNodeItem)
	assert.Equal(t, "oidc", node.GetKind())
	assert.Equal(t, theProject.ProjectID, node.GetProjectId())
	assert.Equal(t, "billingAccounts/0-1/budgets/"+id, node.GetBudget())

	// Pressed again, the node's meter is kept, not made twice.
	_, err := s.connectGoogleCosts(context.Background(), "root-token-3", "google:root-sub")
	require.NoError(t, err)
	b.mu.Lock()
	assert.Len(t, b.order, 1)
	b.mu.Unlock()
}

// An API key is a spend item of its own: a project made for it alone, billed to the
// node's billing account, metered by its own budget, the key sealed in its namespace.
func TestAKeyIsMadeInAProjectOfItsOwn(t *testing.T) {
	s, b, _ := wiredForCosts(t)
	k := keysStandingIn(t)

	said, err := s.connectGoogleKey(context.Background(), "root-token-4", "google:root-sub", "garden")
	require.NoError(t, err)
	assert.Contains(t, said, "garden")

	require.Len(t, k.projects, 1)
	var project string
	for id := range k.projects {
		project = id
	}
	assert.True(t, strings.HasPrefix(project, "qntx-garden-"), project)
	assert.LessOrEqual(t, len(project), 30, "Google's longest project id")
	b.mu.Lock()
	assert.Equal(t, "billingAccounts/0-1", b.linked[project])
	assert.Contains(t, b.enabled, "generativelanguage.googleapis.com")
	assert.Contains(t, b.enabled, "apikeys.googleapis.com")
	b.mu.Unlock()
	number := "9" + project[len(project)-6:]
	meterOf(t, b, number)

	require.Len(t, k.keys, 1)
	k.mu.Lock()
	for name, asked := range k.keys {
		assert.True(t, strings.HasPrefix(name, "projects/"+number+"/locations/global/keys/"), name)
		targets := asked["restrictions"].(map[string]any)["apiTargets"].([]any)
		assert.Equal(t, "generativelanguage.googleapis.com", targets[0].(map[string]any)["service"], "the key reaches Gemini and nothing else")
	}
	k.mu.Unlock()
	value, err := s.keyValue("garden", googleKeyName)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(value, "AIza-projects/"+number), value)

	garden := itemOf(t, rootAsksGoogle(t, s), "garden")
	assert.Equal(t, "key", garden.GetKind())
	assert.Equal(t, project, garden.GetProjectId())

	// Nothing is made at Google for a namespace the node does not serve.
	_, err = s.connectGoogleKey(context.Background(), "root-token-6", "google:root-sub", "nowhere")
	require.Error(t, err)
	assert.Len(t, k.projects, 1)

	// A project made whose key was not kept is finished on the next yes, not made again.
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "google:root-sub"
	require.NoError(t, s.setKeyLine(admitted, "garden", googleKeyName, map[string]any{keyDropped: true}))
	_, err = s.connectGoogleKey(context.Background(), "root-token-7", "google:root-sub", "garden")
	require.NoError(t, err)
	assert.Len(t, k.projects, 1)
	assert.Len(t, k.keys, 2)
	_, err = s.keyValue("garden", googleKeyName)
	require.NoError(t, err)

	// One key per namespace: a second is refused, not made beside it.
	_, err = s.connectGoogleKey(context.Background(), "root-token-5", "google:root-sub", "garden")
	require.Error(t, err)
	assert.Len(t, k.projects, 1)
}

// "each budget halting only its own item": what one item spends halts that item, and
// the others keep running.
func TestABudgetHaltsOnlyItsOwnItem(t *testing.T) {
	s, b, mailed := wiredForCosts(t)
	keysStandingIn(t)
	_, err := s.connectGoogleKey(context.Background(), "root-token-4", "google:root-sub", "garden")
	require.NoError(t, err)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	googleNow = func() time.Time { return now }
	t.Cleanup(func() { googleNow = time.Now })

	require.Nil(t, rootSetsLimit(t, s, sigil.Sent{"amount": "1", "per": "day", "item": "garden"}))
	require.Nil(t, rootSetsLimit(t, s, sigil.Sent{"amount": "1", "per": "day"}))

	garden := itemOf(t, rootAsksGoogle(t, s), "garden")
	gardenMeter := garden.GetBudget()[strings.LastIndex(garden.GetBudget(), "/")+1:]
	nodeMeter, _ := meterOf(t, b, theProject.ProjectNumber)
	// What is spent today is counted from the last report before it.
	b.says(gardenMeter, "a0", "2026-11-19T22:00:00Z", "qntx garden", 2, 0, "2026-11-01T00:00:00Z")
	b.says(nodeMeter, "n0", "2026-11-19T22:00:00Z", "qntx node", 3, 0, "2026-11-01T00:00:00Z")
	b.says(gardenMeter, "a1", "2026-11-20T06:00:00Z", "qntx garden", 2.9, 0, "2026-11-01T00:00:00Z")
	b.says(nodeMeter, "n1", "2026-11-20T06:00:00Z", "qntx node", 3.1, 0, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))

	am := rootAsksGoogle(t, s)
	assert.True(t, itemOf(t, am, "garden").GetHalted(), "0.90 of garden's 1 a day is past 80%")
	assert.Contains(t, itemOf(t, am, "garden").GetHaltedWhy(), "garden")
	assert.False(t, itemOf(t, am, googleNodeItem).GetHalted(), "the node spent 0.10 of its own 1")
	halted, _ := s.googleItemHalted(googleNodeItem)
	assert.False(t, halted, "agy, on the node's identity, keeps running")
	_, err = s.keyValue("garden", googleKeyName)
	require.Error(t, err, "garden's key is not handed out while garden is halted")
	assert.Contains(t, err.Error(), "halted")
	require.Len(t, mailed, 1)
	assert.Contains(t, (<-mailed).Subject, "garden")
}

// A budget of ROOT's halts the items it covers: one scoped to a project halts that
// project's item, and one over the whole account halts every item.
func TestROOTsBudgetsHaltWhatTheyCover(t *testing.T) {
	s, b, _ := wiredForCosts(t)
	keysStandingIn(t)
	_, err := s.connectGoogleKey(context.Background(), "root-token-4", "google:root-sub", "garden")
	require.NoError(t, err)
	garden := itemOf(t, rootAsksGoogle(t, s), "garden")

	b.mu.Lock()
	b.made["gardens-cap"] = map[string]any{"displayName": "gardens cap",
		"amount":       map[string]any{"specifiedAmount": map[string]any{"currencyCode": "EUR", "units": "5"}},
		"budgetFilter": map[string]any{"calendarPeriod": "MONTH", "projects": []any{"projects/" + garden.GetProjectNumber()}}}
	b.order = append(b.order, "gardens-cap")
	b.mu.Unlock()
	b.says("gardens-cap", "c1", "2026-11-20T06:00:00Z", "gardens cap", 4, 5, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	am := rootAsksGoogle(t, s)
	assert.True(t, itemOf(t, am, "garden").GetHalted(), "4 of garden's cap of 5 is 80%")
	assert.False(t, itemOf(t, am, googleNodeItem).GetHalted(), "the cap covers garden's project alone")

	// "monthly" is ROOT's budget over the whole account.
	b.says("monthly", "m1", "2026-11-20T07:00:00Z", "monthly", 16, 20, "2026-11-01T00:00:00Z")
	require.NoError(t, s.googleCostsTick(context.Background()))
	am = rootAsksGoogle(t, s)
	assert.True(t, itemOf(t, am, googleNodeItem).GetHalted(), "the account's budget covers the node too")
	assert.True(t, itemOf(t, am, "garden").GetHalted())
}

// A limit on an item the node has no meter for cannot be counted, and a node that does
// not know what it spends does not spend.
func TestALimitOnAnUnmeteredItemHalts(t *testing.T) {
	s, _ := connectedToGoogle(t)
	b := billingStandingIn(t, s, "billingAccounts/0-1", "monthly")
	require.Nil(t, s.keepGoogleCostLine(googleCostsLineSubject, "wired", "google:root-sub", map[string]any{"billing_account": b.account}))
	require.Nil(t, rootSetsLimit(t, s, sigil.Sent{"amount": "1", "per": "day"}))
	halted, why := s.googleItemHalted(googleNodeItem)
	assert.True(t, halted)
	assert.Contains(t, why, "Connect costs")
}
