package qntxinbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// heldStore is the store a token reaches, keeping what was written.
type heldStore struct {
	token   string
	written []*types.AsCommand
	held    []*types.As
}

func (s *heldStore) GenerateAndCreateAttestation(_ context.Context, cmd *types.AsCommand) (*types.As, error) {
	s.written = append(s.written, cmd)
	as := &types.As{
		ID:         "AS-MAIL-" + strconv.Itoa(len(s.written)),
		Subjects:   cmd.Subjects,
		Predicates: cmd.Predicates,
		Contexts:   cmd.Contexts,
		Actors:     cmd.Actors,
		Attributes: cmd.Attributes,
		Timestamp:  time.Unix(int64(len(s.held)+1), 0),
	}
	s.held = append(s.held, as)
	return as, nil
}

func (s *heldStore) GetAttestations(f ats.AttestationFilter) ([]*types.As, error) {
	matches := func(want, have []string) bool {
		if len(want) == 0 {
			return true
		}
		for _, w := range want {
			if slices.Contains(have, w) {
				return true
			}
		}
		return false
	}
	var out []*types.As
	for _, as := range s.held {
		if matches(f.Subjects, as.Subjects) && matches(f.Predicates, as.Predicates) && matches(f.Contexts, as.Contexts) {
			out = append(out, as)
		}
	}
	return out, nil
}

// grant is as <address> is mail:address of <user>, already held.
func (s *heldStore) grant(address, user string) {
	s.held = append(s.held, &types.As{ID: "AS-GRANT-" + address, Subjects: []string{address}, Predicates: []string{PredicateMailAddress}, Contexts: []string{user}, Timestamp: time.Unix(int64(len(s.held)+1), 0)})
}

func (s *heldStore) wrote(predicate string) []*types.AsCommand {
	var out []*types.AsCommand
	for _, cmd := range s.written {
		if slices.Contains(cmd.Predicates, predicate) {
			out = append(out, cmd)
		}
	}
	return out
}

// rule is the receipt rule's recipients.
type rule struct{ recipients []string }

func (r *rule) Add(_ context.Context, address string) error {
	r.recipients = append(r.recipients, address)
	return nil
}

// outbox keeps what SES was handed.
type outbox struct{ sent []outgoing }

func (s *outbox) Send(_ context.Context, m outgoing) (string, error) {
	s.sent = append(s.sent, m)
	return "ses-" + strconv.Itoa(len(s.sent)), nil
}

func pluginWith(st *heldStore) *Plugin {
	p := NewPlugin()
	p.storeEndpoint = "localhost:50051"
	p.dial = func(_ context.Context, endpoint, token string) (store, func() error, error) {
		st.token = token
		return st, func() error { return nil }, nil
	}
	p.own = func() store { return st }
	p.rule = &rule{}
	p.sender = &outbox{}
	p.bag = &bucket{}
	return p
}

// asked is the request the node hands the plugin for one sigil call.
func asked(body, namespace string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/identity", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Qntx-Asker", "did:key:z6MkTim")
	req.Header.Set("X-Qntx-Store-Token", "call")
	if namespace != "" {
		req.Header.Set("X-Qntx-Namespace", namespace)
	}
	return req
}

func serve(t *testing.T, p *Plugin, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	require.NoError(t, p.RegisterHTTP(mux))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// as <address> is mail:address of <User>, written by the asker the node named.
func TestAnAddressIsAttestedAsTheUsersByTheAsker(t *testing.T) {
	st := &heldStore{}
	w := serve(t, pluginWith(st), asked(`{"user_id":"US-TIM-7K4M3B9X","email":"timothy@example.com"}`, "Clean"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, "call", st.token, "the write went with another token than the call's")
	require.Len(t, st.written, 1)
	written := st.written[0]
	assert.Equal(t, []string{"timothy@example.com"}, written.Subjects)
	assert.Equal(t, []string{"mail:address"}, written.Predicates)
	assert.Equal(t, []string{"US-TIM-7K4M3B9X"}, written.Contexts)
	assert.Equal(t, []string{"did:key:z6MkTim"}, written.Actors)
	assert.JSONEq(t, `{"created":{"id":"AS-MAIL-1","email":"timothy@example.com"}}`, w.Body.String())
}

// A granted address is one SES receives mail for.
func TestAGrantedAddressIsReceivedFor(t *testing.T) {
	p := pluginWith(&heldStore{})
	w := serve(t, p, asked(`{"user_id":"US-TIM-7K4M3B9X","email":"Timothy@example.com"}`, "Clean"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{"timothy@example.com"}, p.rule.(*rule).recipients)
}

// "it will also be namespace specific, and cant be system of default"
func TestNoAddressInSystemOrDefault(t *testing.T) {
	for _, namespace := range []string{"system", "default", ""} {
		st := &heldStore{}
		w := serve(t, pluginWith(st), asked(`{"user_id":"US-TIM-7K4M3B9X","email":"timothy@example.com"}`, namespace))
		assert.Equal(t, http.StatusForbidden, w.Code, "namespace %q: %s", namespace, w.Body.String())
		assert.Empty(t, st.written, "namespace %q took an address", namespace)
	}
}

// "A user can have multiple inboxes"
func TestAUserHoldsMoreThanOneAddress(t *testing.T) {
	st := &heldStore{}
	p := pluginWith(st)
	for _, email := range []string{"timothy@example.com", "contact@example.com"} {
		w := serve(t, p, asked(`{"user_id":"US-TIM-7K4M3B9X","email":"`+email+`"}`, "Clean"))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	require.Len(t, st.written, 2)
	assert.Equal(t, []string{"US-TIM-7K4M3B9X"}, st.written[1].Contexts)
}

// Only a call the node handed can write: no token, no asker, no address.
func TestNoAddressWithoutTheNodesCall(t *testing.T) {
	for _, header := range []string{"X-Qntx-Store-Token", "X-Qntx-Asker"} {
		st := &heldStore{}
		req := asked(`{"user_id":"US-TIM-7K4M3B9X","email":"timothy@example.com"}`, "Clean")
		req.Header.Del(header)
		w := serve(t, pluginWith(st), req)
		assert.Equal(t, http.StatusForbidden, w.Code, "without %s: %s", header, w.Body.String())
		assert.Empty(t, st.written, "without %s an address was written", header)
	}
}

func TestAnAddressNamesAUserAndIsAnAddress(t *testing.T) {
	for _, body := range []string{
		`{"email":"timothy@example.com"}`,
		`{"user_id":"US-TIM-7K4M3B9X"}`,
		`{"user_id":"US-TIM-7K4M3B9X","email":"example.com"}`,
	} {
		st := &heldStore{}
		w := serve(t, pluginWith(st), asked(body, "Clean"))
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", body, w.Body.String())
		assert.Empty(t, st.written, "%s was written", body)
	}
}

// "[At least 7 characters]@domain.tld"
func TestAnAddressHasAtLeastSevenCharactersBeforeTheAt(t *testing.T) {
	for email, status := range map[string]int{
		"abcdef@example.com":  http.StatusBadRequest,
		"abcdefg@example.com": http.StatusOK,
		"ëëëëëëë@example.com": http.StatusOK,
	} {
		st := &heldStore{}
		w := serve(t, pluginWith(st), asked(`{"user_id":"US-TIM-7K4M3B9X","email":"`+email+`"}`, "Clean"))
		assert.Equal(t, status, w.Code, "%s: %s", email, w.Body.String())
	}
}

// QNTX makes the declared routes sigils and MCP tools.
func TestThePluginDeclaresTheRoutes(t *testing.T) {
	var declared []string
	for _, r := range NewPlugin().DeclaredRoutes() {
		declared = append(declared, r.GetMethod()+" "+r.GetPath())
	}
	assert.Equal(t, []string{"POST /identity", "POST /send", "GET /mailbox", "GET /addresses"}, declared)
}

var _ protocol.InboxServiceServer = (*Plugin)(nil)
