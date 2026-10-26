package server

// A key named after a provider is asked of that provider, and the answer is
// the light in Connectors.

// "kept with red light"

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
)

// providersAt is where each provider a namespace keeps a key of answers: the
// base its key is asked at.
var providersAt = map[string]string{
	"anthropic":  "https://api.anthropic.com",
	"openai":     "https://api.openai.com",
	"openrouter": "https://openrouter.ai",
}

// providerKeyAsked is how the provider named is asked whether a key is one it
// issued: a read that needs nothing but the key and spends nothing.
func providerKeyAsked(ctx context.Context, name, base, key string) (*http.Request, error) {
	switch name {
	case "anthropic":
		asked, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
		if err == nil {
			asked.Header.Set("x-api-key", key)
			asked.Header.Set("anthropic-version", "2023-06-01")
		}
		return asked, err
	case "openai":
		asked, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
		if err == nil {
			asked.Header.Set("Authorization", "Bearer "+key)
		}
		return asked, err
	case "openrouter":
		asked, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/key", nil)
		if err == nil {
			asked.Header.Set("Authorization", "Bearer "+key)
		}
		return asked, err
	}
	return nil, errors.Newf("no provider %s to ask a key of", name)
}

// providerKeyTook is how long a provider is waited on.
const providerKeyTook = 10 * time.Second

// checkProviderKey asks the provider named whether it accepts key.
func (s *QNTXServer) checkProviderKey(ctx context.Context, name, base, key string) error {
	ctx, cancel := context.WithTimeout(ctx, providerKeyTook)
	defer cancel()
	asked, err := providerKeyAsked(ctx, name, base, key)
	if err != nil {
		return errors.Wrapf(err, "failed to ask %s at %s about the key", name, base)
	}
	answered, err := http.DefaultClient.Do(asked)
	if err != nil {
		return errors.Wrapf(err, "%s at %s was not reached to ask about the key", name, asked.URL)
	}
	defer answered.Body.Close()
	if answered.StatusCode == http.StatusOK {
		return nil
	}
	said, _ := io.ReadAll(io.LimitReader(answered.Body, 512))
	return errors.Newf("%s answered %d at %s: %s", name, answered.StatusCode, asked.URL, said)
}

// askProviders asks the provider each key of kept is named after whether it
// accepts the key, all at once, and writes the answer on the key. A key named
// after no provider is asked of nobody.
func (s *QNTXServer) askProviders(ctx context.Context, kept *protocol.NamespaceKeys) {
	at := s.providersAt
	if at == nil {
		at = providersAt
	}
	var asking sync.WaitGroup
	for _, key := range kept.GetKeys() {
		base, provider := at[key.GetName()]
		if !provider {
			continue
		}
		key.Asked = true
		value, err := s.keyValue(kept.GetNamespace(), key.GetName())
		if err != nil {
			key.Why = err.Error()
			continue
		}
		asking.Add(1)
		go func(key *protocol.NamespaceKey, base, value string) {
			defer asking.Done()
			if err := s.checkProviderKey(ctx, key.GetName(), base, value); err != nil {
				key.Why = err.Error()
				return
			}
			key.Accepted = true
		}(key, base, value)
	}
	asking.Wait()
}
