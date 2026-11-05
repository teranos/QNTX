package server

// 「[Opus 5.5] What spends at Google, one item at a time: the node's own identity, and each」
// 「[Opus 5.5] namespace's key, each in a project of its own with a budget metering it alone, so」
// 「[Opus 5.5] a budget halts the items it covers and no other.」
// "I see, daily spend does need to be per spend-item"
// "So per subscription or per apikey, or per oidc"
// "one project per Google spend item (each API key, the OIDC identity), with each budget halting only its own item."

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] Where Google makes projects and keys. Vars so a test points them at a stand-in.」
var (
	googleProjects = "https://cloudresourcemanager.googleapis.com"
	googleAPIKeys  = "https://apikeys.googleapis.com"
)

const (
	// 「[Opus 5.5] googleNodeItem is the node's own identity as a spend item: what agy spends.」
	googleNodeItem = "node"
	// 「[Opus 5.5] googleKeyName is the namespace key a key made at Google is kept as.」
	googleKeyName = "gemini"
	// 「[Opus 5.5] googleKeyService is the one API a key made at Google reaches.」
	googleKeyService = "generativelanguage.googleapis.com"
	// 「[Opus 5.5] googleSpendLineSubject is the line of one spend item: its project, and its meter.」
	googleSpendLineSubject = "GOOGLE-SPEND"

	googleItemOIDC = "oidc"
	googleItemKey  = "key"
)

// 「[Opus 5.5] googleKeyServices are what a key's project runs: its keys, and Gemini.」
var googleKeyServices = []string{"apikeys.googleapis.com", googleKeyService}

// 「[Opus 5.5] googleSpendItem is one thing that spends at Google, in its own project.」
type googleSpendItem struct {
	name, kind string
	project    googleProject
	// 「[Opus 5.5] budget is the budget metering the project, by its name at Google; empty」
	// 「[Opus 5.5] is a node item made before items were metered.」
	budget string
}

// 「[Opus 5.5] meter is the id the budget's reports carry.」
func (i googleSpendItem) meter() string {
	return i.budget[strings.LastIndex(i.budget, "/")+1:]
}

// 「[Opus 5.5] called is the item as ROOT reads it in a mail.」
func (i googleSpendItem) called() string {
	if i.name == googleNodeItem {
		return "the node"
	}
	return i.name + "'s key"
}

// 「[Opus 5.5] googleSpendItems is every item: the node's, which a connected node always has,」
// 「[Opus 5.5] and each key made, by the newest line of each.」
func (s *QNTXServer) googleSpendItems() ([]googleSpendItem, error) {
	kept, held, err := s.googleLine()
	if err != nil || !held {
		return nil, err
	}
	lines, err := s.googleCostLines(googleSpendLineSubject)
	if err != nil {
		return nil, err
	}
	items := []googleSpendItem{{name: googleNodeItem, kind: googleItemOIDC, project: kept.project}}
	seen := map[string]bool{}
	for _, line := range lines {
		text := func(name string) string { held, _ := line.Attributes[name].(string); return held }
		name := text("item")
		if seen[name] {
			continue
		}
		seen[name] = true
		item := googleSpendItem{name: name, kind: text("kind"), budget: text("budget"),
			project: googleProject{ProjectID: text("project_id"), ProjectNumber: text("project_number")}}
		if name == googleNodeItem {
			// 「[Opus 5.5] The node's project is the one it is connected in, whatever the line says.」
			if item.project.ProjectID == kept.project.ProjectID {
				items[0].budget = item.budget
			}
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// 「[Opus 5.5] googleSpendItemNamed is the item of that name, or false.」
func (s *QNTXServer) googleSpendItemNamed(name string) (googleSpendItem, bool, error) {
	items, err := s.googleSpendItems()
	if err != nil {
		return googleSpendItem{}, false, err
	}
	for _, item := range items {
		if item.name == name {
			return item, true, nil
		}
	}
	return googleSpendItem{}, false, nil
}

// 「[Opus 5.5] keepSpendItem writes the item's line.」
func (s *QNTXServer) keepSpendItem(item googleSpendItem, by string) error {
	if refused := s.keepGoogleCostLine(googleSpendLineSubject, "item", by, map[string]any{
		"item": item.name, "kind": item.kind, "budget": item.budget,
		"project_id": item.project.ProjectID, "project_number": item.project.ProjectNumber,
	}); refused != nil {
		return errors.New(refused.Says)
	}
	return nil
}

// 「[Opus 5.5] meterProject makes a budget of account metering project alone, reporting to topic.」
// 「[Opus 5.5] Its amount is last period's: a meter, not a cap, for ROOT's limits are what halt.」
func meterProject(ctx context.Context, token, account, topic, called string, project googleProject) (string, error) {
	var made struct {
		Name string `json:"name"`
	}
	if err := googleCall(ctx, token, http.MethodPost, googleBudgets+"/v1/"+account+"/budgets", map[string]any{
		"displayName": called,
		"budgetFilter": map[string]any{
			"projects":       []string{"projects/" + project.ProjectNumber},
			"calendarPeriod": "MONTH",
		},
		"amount":            map[string]any{"lastPeriodAmount": map[string]any{}},
		"notificationsRule": map[string]any{"pubsubTopic": topic, "schemaVersion": "1.0"},
	}, &made); err != nil {
		return "", errors.Wrapf(err, "%s was not metered in %s", project.ProjectID, account)
	}
	if made.Name == "" {
		return "", errors.Newf("Google named no budget it made for %s in %s", project.ProjectID, account)
	}
	return made.Name, nil
}

// 「[Opus 5.5] meterTheNode makes the node's own meter, once: its project, alone.」
func (s *QNTXServer) meterTheNode(ctx context.Context, token, account, topic, by string, kept googleKept) error {
	node, _, err := s.googleSpendItemNamed(googleNodeItem)
	if err != nil {
		return err
	}
	if node.budget != "" {
		return nil
	}
	if node.budget, err = meterProject(ctx, token, account, topic, "qntx node", kept.project); err != nil {
		return err
	}
	return s.keepSpendItem(node, by)
}

// 「[Opus 5.5] googleKeyProject is the id of a new project for namespace's key: qntx-, the」
// 「[Opus 5.5] namespace as far as Google's 30 characters let it, and 6 random hex.」
func googleKeyProject(namespace string) (string, error) {
	var cleaned strings.Builder
	for _, r := range strings.ToLower(namespace) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cleaned.WriteRune(r)
		default:
			cleaned.WriteRune('-')
		}
	}
	name := strings.Trim(cleaned.String(), "-")
	if len(name) > 18 {
		name = strings.TrimRight(name[:18], "-")
	}
	if name == "" {
		name = "ns"
	}
	random := make([]byte, 3)
	if _, err := rand.Read(random); err != nil {
		return "", errors.Wrap(err, "no randomness to name a project with")
	}
	return "qntx-" + name + "-" + hex.EncodeToString(random), nil
}

// 「[Opus 5.5] makeProject makes the project id at Google, and waits for its number.」
func makeProject(ctx context.Context, token, id, called string) (googleProject, error) {
	var op googleOperation
	if err := googleCall(ctx, token, http.MethodPost, googleProjects+"/v3/projects", map[string]any{"projectId": id, "displayName": called}, &op); err != nil {
		return googleProject{}, errors.Wrapf(err, "the project %s was not made", id)
	}
	if err := waitAt(ctx, token, googleProjects+"/v3/", op); err != nil {
		return googleProject{}, err
	}
	var made struct {
		Name      string `json:"name"`
		ProjectID string `json:"projectId"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleProjects+"/v3/projects/"+url.PathEscape(id), nil, &made); err != nil {
		return googleProject{}, errors.Wrapf(err, "the project %s made was not read", id)
	}
	number := strings.TrimPrefix(made.Name, "projects/")
	if number == "" || number == made.Name {
		return googleProject{}, errors.Newf("Google named the project %s made %q, not projects/<number>", id, made.Name)
	}
	return googleProject{ProjectID: id, ProjectNumber: number, Name: called}, nil
}

// 「[Opus 5.5] mintKey makes a key in project reaching Gemini alone, and reads its value.」
func mintKey(ctx context.Context, token, called string, project googleProject) (string, error) {
	var op struct {
		Name     string `json:"name"`
		Done     bool   `json:"done"`
		Response struct {
			Name string `json:"name"`
		} `json:"response"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	at := googleAPIKeys + "/v2/projects/" + project.ProjectNumber + "/locations/global/keys"
	if err := googleCall(ctx, token, http.MethodPost, at, map[string]any{
		"displayName":  called,
		"restrictions": map[string]any{"apiTargets": []map[string]any{{"service": googleKeyService}}},
	}, &op); err != nil {
		return "", errors.Wrapf(err, "no key was made in %s", project.ProjectID)
	}
	for !op.Done {
		select {
		case <-ctx.Done():
			return "", errors.Wrapf(ctx.Err(), "Google did not finish %s in time", op.Name)
		case <-time.After(googleAsksAgain):
		}
		if err := googleCall(ctx, token, http.MethodGet, googleAPIKeys+"/v2/"+op.Name, nil, &op); err != nil {
			return "", err
		}
	}
	if op.Error != nil {
		return "", errors.Newf("Google did not make the key in %s: %s", project.ProjectID, op.Error.Message)
	}
	var value struct {
		KeyString string `json:"keyString"`
	}
	if err := googleCall(ctx, token, http.MethodGet, googleAPIKeys+"/v2/"+op.Response.Name+"/keyString", nil, &value); err != nil {
		return "", errors.Wrapf(err, "the key %s made was not read", op.Response.Name)
	}
	if value.KeyString == "" {
		return "", errors.Newf("Google answered no value for the key %s", op.Response.Name)
	}
	return value.KeyString, nil
}

// 「[Opus 5.5] connectGoogleKey makes namespace's key on ROOT's token: a project of its own,」
// 「[Opus 5.5] billed to the node's billing account and metered alone, and a key in it reaching」
// 「[Opus 5.5] Gemini, sealed as the namespace's key gemini. ROOT's token is let go once the node」
// 「[Opus 5.5] no longer needs it.」
func (s *QNTXServer) connectGoogleKey(ctx context.Context, token, by, namespace string) (said string, err error) {
	defer s.letGoOfGoogle(token)
	defer func() { err = googleFixOf(err) }()
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || namespace == googleNodeItem {
		return "", errors.Newf("a key is made for a namespace, and %q names none", namespace)
	}
	// 「[Opus 5.5] Nothing is made at Google for a namespace the node does not serve.」
	if namespace, err = s.serves(namespace); err != nil {
		return "", err
	}
	kept, held, err := s.googleLine()
	if err != nil {
		return "", err
	}
	if !held {
		return "", &auth.GoogleCloudFix{Says: "Connect this node to Google Cloud first, in Node Connectors.", Full: "the node has no GOOGLE line"}
	}
	lines, err := s.googleCostLines(googleCostsLineSubject)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", &auth.GoogleCloudFix{Says: "Connect costs first: a key is made only where what it spends is read.", Full: "the node has no " + googleCostsLineSubject + " line"}
	}
	account, _ := lines[0].Attributes["billing_account"].(string)
	if account == "" {
		return "", errors.Newf("%s line %s names no billing account", googleCostsLineSubject, lines[0].ID)
	}
	called := "QNTX " + namespace
	if len(called) > 30 {
		called = called[:30]
	}
	item, found, err := s.googleSpendItemNamed(namespace)
	if err != nil {
		return "", err
	}
	if found {
		// 「[Opus 5.5] A project made and metered whose key was not kept is finished, not made twice.」
		keys, err := s.newestKeys(namespace)
		if err != nil {
			return "", err
		}
		if line, held := keys[googleKeyName]; held {
			if _, sealed := line.Attributes[keySealed].(string); sealed {
				return "", errors.Newf("%s has its key at Google already, in %s", namespace, item.project.ProjectID)
			}
		}
	} else {
		id, err := googleKeyProject(namespace)
		if err != nil {
			return "", err
		}
		project, err := makeProject(ctx, token, id, called)
		if err != nil {
			return "", err
		}
		if err := googleCall(ctx, token, http.MethodPut, googleBilling+"/v1/projects/"+url.PathEscape(project.ProjectID)+"/billingInfo", map[string]any{"billingAccountName": account}, nil); err != nil {
			return "", errors.Wrapf(err, "%s was not linked to %s", project.ProjectID, account)
		}
		if err := enableServices(ctx, token, project.ProjectID, googleKeyServices); err != nil {
			return "", err
		}
		item = googleSpendItem{name: namespace, kind: googleItemKey, project: project}
		topic := "projects/" + kept.project.ProjectID + "/topics/" + googleCostsTopic
		if item.budget, err = meterProject(ctx, token, account, topic, "qntx "+namespace, project); err != nil {
			return "", err
		}
		// 「[Opus 5.5] The item is kept before its key is: a key that spends is never one the node does not meter.」
		if err := s.keepSpendItem(item, by); err != nil {
			return "", err
		}
	}
	project := item.project
	value, err := mintKey(ctx, token, called, project)
	if err != nil {
		return "", err
	}
	sealed, err := s.sealKey(namespace, value)
	if err != nil {
		return "", err
	}
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = by
	if err := s.setKeyLine(admitted, namespace, googleKeyName, map[string]any{keySealed: sealed}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s has its own key at Google, kept as its key %s. It spends in %s alone, billed to %s, and halts on its own limits.", namespace, googleKeyName, project.ProjectID, account), nil
}

// 「[Opus 5.5] googleItemHalted is whether what item spends at Google is halted, and why: a budget」
// 「[Opus 5.5] of ROOT's that covers its project at 80%, or what its own meter counts against」
// 「[Opus 5.5] ROOT's limits on it. What cannot be read counts as halted, for a node that does not」
// 「[Opus 5.5] know what it spends does not spend.」
func (s *QNTXServer) googleItemHalted(name string) (bool, string) {
	wired, err := s.googleCostsWired()
	if err != nil {
		return true, err.Error()
	}
	if !wired {
		return false, ""
	}
	items, err := s.googleSpendItems()
	if err != nil {
		return true, err.Error()
	}
	var item googleSpendItem
	meters := map[string]bool{}
	for _, each := range items {
		if each.budget != "" {
			meters[each.meter()] = true
		}
		if each.name == name {
			item = each
		}
	}
	if item.name == "" {
		return true, "there is no spend item " + name + " at Google"
	}
	reports, err := s.googleBudgetReports()
	if err != nil {
		return true, err.Error()
	}
	s.googleHaltMu.Lock()
	set := s.googleBudgetsSet
	s.googleHaltMu.Unlock()
	for _, report := range reports {
		if meters[report.id] {
			continue
		}
		if !budgetCovers(set, report.id, item.project) {
			continue
		}
		if report.budget > 0 && report.cost >= report.budget*googleHaltsAt {
			return true, fmt.Sprintf("%s has reached %.2f %s of %.2f, and what %s spends halts at %d%% of it", report.name, report.cost, report.currency, report.budget, item.called(), int(googleHaltsAt*100))
		}
	}
	limit, err := s.googleItemLimit(googleLimitLineSubject, name)
	if err != nil {
		return true, err.Error()
	}
	day, err := s.googleItemLimit(googleDayLimitLineSubject, name)
	if err != nil {
		return true, err.Error()
	}
	if limit <= 0 && day <= 0 {
		return false, ""
	}
	if item.budget == "" {
		return true, fmt.Sprintf("ROOT set a limit on %s, and the node does not meter what it spends: ROOT presses Connect costs again", item.called())
	}
	if limit > 0 {
		cost, currency := 0.0, ""
		for _, report := range reports {
			if report.id == item.meter() {
				cost, currency = report.cost, report.currency
			}
		}
		if cost >= limit*googleHaltsAt {
			return true, fmt.Sprintf("%s has spent %.2f %s of its limit of %.2f, and halts at %d%% of it", item.called(), cost, currency, limit, int(googleHaltsAt*100))
		}
	}
	if day > 0 {
		spent, on, err := s.googleSpentTodayOn(item.meter())
		if err != nil {
			return true, err.Error()
		}
		if spent >= day*googleHaltsAt {
			return true, fmt.Sprintf("%s has spent %.2f %s today of %.2f, and halts at %d%% of it until tomorrow", item.called(), spent, on.currency, day, int(googleHaltsAt*100))
		}
	}
	return false, ""
}

// 「[Opus 5.5] budgetCovers is whether the budget of id counts what project spends: one over」
// 「[Opus 5.5] the whole account, one naming the project, or one whose scope is not read yet.」
func budgetCovers(set []googleBudgetSet, id string, project googleProject) bool {
	for _, each := range set {
		if each.id != id {
			continue
		}
		return len(each.projects) == 0 || slices.Contains(each.projects, "projects/"+project.ProjectNumber)
	}
	return true
}
