package qntxatproto

import (
	"context"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/teranos/QNTX/errors"
)

// resolveHandle resolves a handle to a DID using the given XRPC client.
func resolveHandle(ctx context.Context, client *xrpc.Client, handle string) (string, error) {
	resp, err := comatproto.IdentityResolveHandle(ctx, client, handle)
	if err != nil {
		return "", errors.Wrapf(err, "failed to resolve handle %s", handle)
	}
	return resp.Did, nil
}
