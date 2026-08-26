package auth

// atproto: an app password, spent once, against a PDS the DID vouches for.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/teranos/errors"
)

func atprotoConfirm(ctx context.Context, host, identifier, secret string) (account, error) {
	handle := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(identifier)), "@")
	if handle == "" {
		return account{}, errors.New("a handle is required")
	}
	if secret == "" {
		return account{}, errors.New("an app password is required")
	}

	body, err := json.Marshal(map[string]string{"identifier": handle, "password": secret})
	if err != nil {
		return account{}, errors.Wrap(err, "failed to build the createSession request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+host+"/xrpc/com.atproto.server.createSession", strings.NewReader(string(body)))
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build createSession for %s", host)
	}
	req.Header.Set("Content-Type", "application/json")

	var session struct {
		DID    string `json:"did"`
		Handle string `json:"handle"`
	}
	if err := getJSON(req, "createSession", &session); err != nil {
		return account{}, err
	}
	if session.DID == "" {
		return account{}, errors.Newf("createSession against %s returned no DID", host)
	}

	// Anyone can run a PDS and have it answer with any DID. The DID document is
	// the only thing that says which host speaks for a DID, so it is asked —
	// otherwise a listed did:plc could be claimed by a host that made it up.
	vouched, err := atprotoPDSHost(ctx, session.DID)
	if err != nil {
		return account{}, err
	}
	if vouched != host {
		return account{}, errors.Newf(
			"%s answered for %s, but that DID's document names %s as its PDS",
			host, session.DID, vouched)
	}

	return account{CanonicalID: session.DID, Handle: "@" + session.Handle}, nil
}

// atprotoPDSHost reads a DID document and returns the host it names as the
// account's PDS. This is the check that makes an atproto binding mean anything.
func atprotoPDSHost(ctx context.Context, did string) (string, error) {
	var docURL string
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		docURL = "https://plc.directory/" + did
	case strings.HasPrefix(did, "did:web:"):
		host, err := normalizeHost(strings.TrimPrefix(did, "did:web:"))
		if err != nil {
			return "", errors.Wrapf(err, "%s does not name a resolvable host", did)
		}
		docURL = "https://" + host + "/.well-known/did.json"
	default:
		return "", errors.Newf("%s is neither did:plc nor did:web, so nothing here can resolve it", did)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return "", errors.Wrapf(err, "failed to build the DID document request for %s", did)
	}

	var doc struct {
		Service []struct {
			ID              string `json:"id"`
			Type            string `json:"type"`
			ServiceEndpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	if err := getJSON(req, "DID document", &doc); err != nil {
		return "", err
	}

	for _, svc := range doc.Service {
		if svc.Type != "AtprotoPersonalDataServer" && !strings.HasSuffix(svc.ID, "#atproto_pds") {
			continue
		}
		host, err := normalizeHost(svc.ServiceEndpoint)
		if err != nil {
			return "", errors.Wrapf(err, "the DID document for %s names an unusable PDS endpoint %q", did, svc.ServiceEndpoint)
		}
		return host, nil
	}
	return "", errors.Newf("the DID document for %s names no personal data server", did)
}
