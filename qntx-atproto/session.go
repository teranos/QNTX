package qntxatproto

import (
	"context"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/teranos/QNTX/errors"
)

// createSession authenticates with a PDS and returns an authenticated XRPC client.
func createSession(ctx context.Context, pdsHost, identifier, appPassword string) (*xrpc.Client, string, error) {
	client := &xrpc.Client{
		Host: pdsHost,
	}

	input := &comatproto.ServerCreateSession_Input{
		Identifier: identifier,
		Password:   appPassword,
	}

	session, err := comatproto.ServerCreateSession(ctx, client, input)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to create session with PDS %s for %s", pdsHost, identifier)
	}

	client.Auth = &xrpc.AuthInfo{
		AccessJwt:  session.AccessJwt,
		RefreshJwt: session.RefreshJwt,
		Handle:     session.Handle,
		Did:        session.Did,
	}

	return client, session.Did, nil
}

// refreshSession refreshes an existing XRPC session.
func refreshSession(ctx context.Context, client *xrpc.Client) error {
	if client.Auth == nil {
		return errors.New("no auth session to refresh")
	}

	// Use refresh token
	refreshClient := &xrpc.Client{
		Host: client.Host,
		Auth: &xrpc.AuthInfo{
			AccessJwt: client.Auth.RefreshJwt,
		},
	}

	session, err := comatproto.ServerRefreshSession(ctx, refreshClient)
	if err != nil {
		return errors.Wrapf(err, "failed to refresh session at %s", client.Host)
	}

	client.Auth.AccessJwt = session.AccessJwt
	client.Auth.RefreshJwt = session.RefreshJwt
	client.Auth.Handle = session.Handle
	client.Auth.Did = session.Did

	return nil
}
