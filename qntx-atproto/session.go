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

// TODO: Implement session refresh to handle token expiry (~2 hours).
// Options: (a) retry on 401 with refresh token, or (b) background goroutine.
// Without this, plugin breaks on token expiry until restart.
