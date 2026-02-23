package qntxatproto

import (
	"context"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/teranos/QNTX/errors"
)

// syncTimeline is a Pulse job that fetches the timeline and creates attestations.
func (p *Plugin) syncTimeline(ctx context.Context, jobID string) error {
	logger := p.services.Logger("atproto")

	// Skip if paused
	if p.paused {
		logger.Debug("Timeline sync skipped (plugin paused)")
		return nil
	}

	// Skip if not authenticated
	client := p.getClient()
	if client == nil {
		logger.Debug("Timeline sync skipped (not authenticated)")
		return nil
	}

	config := p.services.Config("atproto")
	limit := int64(config.GetInt("timeline_sync_limit"))
	if limit <= 0 {
		limit = 50 // Default
	}
	if limit > 100 {
		limit = 100 // Cap at Bluesky API limit
	}

	logger.Infow("Starting timeline sync", "limit", limit)

	// Fetch timeline (use request context for this)
	resp, err := appbsky.FeedGetTimeline(ctx, client, "", "", limit)
	if err != nil {
		return errors.Wrap(err, "failed to fetch timeline")
	}

	// Create attestations with a longer timeout
	// Use background context with timeout instead of request context
	// to avoid cancellation while creating attestations
	attestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Create attestations for each post
	postCount := 0
	errorCount := 0
	for _, feedItem := range resp.Feed {
		if feedItem.Post == nil {
			continue
		}

		post := feedItem.Post
		if post.Record == nil {
			continue
		}

		// Extract post data
		uri := post.Uri
		cid := post.Cid
		authorDID := post.Author.Did
		authorHandle := post.Author.Handle

		// Extract text from record
		text := ""
		if recordVal, ok := post.Record.Val.(*appbsky.FeedPost); ok {
			text = recordVal.Text
		}

		// Create attestation (use attestation context, not request context)
		if err := p.attestTimelinePost(attestCtx, uri, authorDID, authorHandle, text, cid); err != nil {
			logger.Warnw("Failed to create timeline post attestation",
				"uri", uri,
				"author", authorHandle,
				"error", err)
			errorCount++
		} else {
			postCount++
		}
	}

	logger.Infow("Timeline sync complete", "posts_indexed", postCount, "errors", errorCount)

	// Return error if all attestations failed
	if errorCount > 0 && postCount == 0 {
		return errors.Newf("timeline sync failed: all %d attestations failed", errorCount)
	}
	return nil
}
