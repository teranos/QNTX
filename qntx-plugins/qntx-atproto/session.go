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

// refreshSession uses the refresh JWT to obtain new access and refresh tokens.
// The AT Protocol refresh endpoint requires the refresh token as the Bearer token.
func refreshSession(ctx context.Context, client *xrpc.Client) error {
	if client.Auth == nil || client.Auth.RefreshJwt == "" {
		return errors.New("no refresh token available")
	}

	// ServerRefreshSession sends Bearer <AccessJwt>, so temporarily swap in the refresh token
	savedAccess := client.Auth.AccessJwt
	client.Auth.AccessJwt = client.Auth.RefreshJwt

	session, err := comatproto.ServerRefreshSession(ctx, client)
	if err != nil {
		// Restore original token on failure so the client state isn't corrupted
		client.Auth.AccessJwt = savedAccess
		return errors.Wrap(err, "failed to refresh session")
	}

	client.Auth.AccessJwt = session.AccessJwt
	client.Auth.RefreshJwt = session.RefreshJwt
	client.Auth.Handle = session.Handle
	client.Auth.Did = session.Did
	return nil
}

// isExpiredToken checks if an error is an AT Protocol ExpiredToken error.
// The PDS returns HTTP 400 with {"error": "ExpiredToken"} when the access token has expired.
func isExpiredToken(err error) bool {
	var xrpcErr *xrpc.Error
	if !errors.As(err, &xrpcErr) {
		return false
	}
	var inner *xrpc.XRPCError
	if errors.As(xrpcErr.Wrapped, &inner) {
		return inner.ErrStr == "ExpiredToken"
	}
	return false
}
