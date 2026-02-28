package qntxatproto

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/teranos/QNTX/errors"
)

// registerHTTPHandlers registers all HTTP handlers for the atproto domain.
// QNTX strips the /api/atproto prefix before forwarding requests to the plugin.
func (p *Plugin) registerHTTPHandlers(mux *http.ServeMux) error {
	// Profile
	mux.HandleFunc("GET /profile", p.handleProfile)
	mux.HandleFunc("GET /profile/", p.handleActorProfile)

	// Timeline and feeds
	mux.HandleFunc("GET /timeline", p.handleTimeline)
	mux.HandleFunc("GET /feed/", p.handleAuthorFeed)

	// Write operations
	mux.HandleFunc("POST /post", p.handleCreatePost)
	mux.HandleFunc("POST /follow", p.handleFollow)
	mux.HandleFunc("POST /like", p.handleLike)

	// Identity
	mux.HandleFunc("GET /resolve/", p.handleResolve)

	// Notifications
	mux.HandleFunc("GET /notifications", p.handleNotifications)

	// Timeline sync (for Pulse scheduling or manual triggering)
	mux.HandleFunc("POST /sync-timeline", p.handleSyncTimeline)

	// AT Protocol feed glyph
	mux.HandleFunc("GET /feed-glyph", p.handleFeedGlyph)
	mux.HandleFunc("GET /feed-glyph.css", p.handleFeedGlyphCSS)

	return nil
}

// checkPaused returns true and writes 503 if plugin is paused.
func (p *Plugin) checkPaused(w http.ResponseWriter) bool {
	if p.IsPaused() {
		writeError(w, http.StatusServiceUnavailable, "AT Protocol plugin is paused")
		return true
	}
	return false
}

// requireAuth returns the authenticated client, or writes 401 and returns nil.
func (p *Plugin) requireAuth(w http.ResponseWriter) bool {
	if p.getClient() == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated — configure identifier and app_password")
		return false
	}
	return true
}

// --- Read handlers ---

// handleProfile returns the authenticated user's profile.
func (p *Plugin) handleProfile(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	var profile *appbsky.ActorDefs_ProfileViewDetailed
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		profile, err = appbsky.ActorGetProfile(r.Context(), c, c.Auth.Did)
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to get profile", "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to get profile: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// handleActorProfile returns any actor's profile.
// Path: /profile/{actor}
func (p *Plugin) handleActorProfile(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	actor := strings.TrimPrefix(r.URL.Path, "/profile/")
	if actor == "" {
		writeError(w, http.StatusBadRequest, "Actor handle or DID required")
		return
	}

	var profile *appbsky.ActorDefs_ProfileViewDetailed
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		profile, err = appbsky.ActorGetProfile(r.Context(), c, actor)
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to get actor profile", "actor", actor, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to get profile for %s: %v", actor, err))
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// handleTimeline returns the authenticated user's home timeline.
func (p *Plugin) handleTimeline(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	limit := int64(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	var resp *appbsky.FeedGetTimeline_Output
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		resp, err = appbsky.FeedGetTimeline(r.Context(), c, "", cursor, limit)
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to get timeline", "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to get timeline: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleAuthorFeed returns an actor's feed.
// Path: /feed/{actor}
func (p *Plugin) handleAuthorFeed(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	actor := strings.TrimPrefix(r.URL.Path, "/feed/")
	if actor == "" {
		writeError(w, http.StatusBadRequest, "Actor handle or DID required")
		return
	}

	limit := int64(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	var resp *appbsky.FeedGetAuthorFeed_Output
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		resp, err = appbsky.FeedGetAuthorFeed(r.Context(), c, actor, cursor, "", false, limit)
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to get author feed", "actor", actor, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to get feed for %s: %v", actor, err))
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleNotifications returns the authenticated user's notifications.
func (p *Plugin) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	limit := int64(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.ParseInt(l, 10, 64); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	cursor := r.URL.Query().Get("cursor")

	var resp *appbsky.NotificationListNotifications_Output
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		resp, err = appbsky.NotificationListNotifications(r.Context(), c, cursor, limit, false, nil, "")
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to get notifications", "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to get notifications: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleResolve resolves a handle to a DID.
// Path: /resolve/{handle}
func (p *Plugin) handleResolve(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}

	handle := strings.TrimPrefix(r.URL.Path, "/resolve/")
	if handle == "" {
		writeError(w, http.StatusBadRequest, "Handle required")
		return
	}

	// Resolution works without authentication — use unauthenticated client if needed
	client := p.getClient()
	if client == nil {
		config := p.Services().Config("atproto")
		pdsHost := config.GetString("pds_host")
		if pdsHost == "" {
			pdsHost = "https://bsky.social"
		}
		client = &xrpc.Client{Host: pdsHost}
	}

	did, err := resolveHandle(r.Context(), client, handle)
	if err != nil {
		err = errors.WithDetail(err, fmt.Sprintf("Handle: %s", handle))
		p.Services().Logger("atproto").Errorw("Failed to resolve handle", "handle", handle, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to resolve handle %s: %v", handle, err))
		return
	}

	p.attestResolve(handle, did)

	writeJSON(w, http.StatusOK, map[string]string{
		"handle": handle,
		"did":    did,
	})
}

// --- Write handlers ---

// CreatePostRequest is the request body for creating a post.
type CreatePostRequest struct {
	Text     string `json:"text"`
	ReplyTo  string `json:"reply_to,omitempty"`  // AT URI of immediate parent post
	ReplyCID string `json:"reply_cid,omitempty"` // CID of immediate parent post
	RootURI  string `json:"root_uri,omitempty"`  // AT URI of thread root (required for deep replies)
	RootCID  string `json:"root_cid,omitempty"`  // CID of thread root (required for deep replies)
}

// handleCreatePost creates a new post.
func (p *Plugin) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	var req CreatePostRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "Text is required")
		return
	}

	post := &appbsky.FeedPost{
		Text:      req.Text,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	// Set reply reference if provided
	if req.ReplyTo != "" && req.ReplyCID != "" {
		// Parent is always the immediate parent
		parent := &comatproto.RepoStrongRef{
			Uri: req.ReplyTo,
			Cid: req.ReplyCID,
		}

		// Root is either explicitly provided (for deep replies) or same as parent (depth-1 replies)
		root := parent
		if req.RootURI != "" && req.RootCID != "" {
			root = &comatproto.RepoStrongRef{
				Uri: req.RootURI,
				Cid: req.RootCID,
			}
		}

		post.Reply = &appbsky.FeedPost_ReplyRef{
			Parent: parent,
			Root:   root,
		}
	}

	var resp *comatproto.RepoCreateRecord_Output
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		resp, err = comatproto.RepoCreateRecord(r.Context(), c, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.feed.post",
			Repo:       c.Auth.Did,
			Record:     &util.LexiconTypeDecoder{Val: post},
		})
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to create post", "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to create post: %v", err))
		return
	}

	p.attestPost(p.getDID(), resp.Uri, resp.Cid, req.Text)

	writeJSON(w, http.StatusCreated, map[string]string{
		"uri": resp.Uri,
		"cid": resp.Cid,
	})
}

// FollowRequest is the request body for following an actor.
type FollowRequest struct {
	Subject string `json:"subject"` // DID of actor to follow
}

// handleFollow follows an actor.
func (p *Plugin) handleFollow(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	var req FollowRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.Subject == "" {
		writeError(w, http.StatusBadRequest, "Subject DID is required")
		return
	}

	follow := &appbsky.GraphFollow{
		Subject:   req.Subject,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	var resp *comatproto.RepoCreateRecord_Output
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		resp, err = comatproto.RepoCreateRecord(r.Context(), c, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.graph.follow",
			Repo:       c.Auth.Did,
			Record:     &util.LexiconTypeDecoder{Val: follow},
		})
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to follow actor", "subject", req.Subject, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to follow %s: %v", req.Subject, err))
		return
	}

	p.attestFollow(p.getDID(), req.Subject, resp.Uri)

	writeJSON(w, http.StatusCreated, map[string]string{
		"uri": resp.Uri,
		"cid": resp.Cid,
	})
}

// LikeRequest is the request body for liking a post.
type LikeRequest struct {
	URI string `json:"uri"` // AT URI of post to like
	CID string `json:"cid"` // CID of post to like
}

// handleLike likes a post.
func (p *Plugin) handleLike(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	var req LikeRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.URI == "" || req.CID == "" {
		writeError(w, http.StatusBadRequest, "URI and CID are required")
		return
	}

	like := &appbsky.FeedLike{
		Subject: &comatproto.RepoStrongRef{
			Uri: req.URI,
			Cid: req.CID,
		},
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	var resp *comatproto.RepoCreateRecord_Output
	if err := p.doWithRefresh(r.Context(), func(c *xrpc.Client) error {
		var err error
		resp, err = comatproto.RepoCreateRecord(r.Context(), c, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.feed.like",
			Repo:       c.Auth.Did,
			Record:     &util.LexiconTypeDecoder{Val: like},
		})
		return err
	}); err != nil {
		p.Services().Logger("atproto").Errorw("Failed to like post", "uri", req.URI, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to like post: %v", err))
		return
	}

	p.attestLike(p.getDID(), req.URI, resp.Uri)

	writeJSON(w, http.StatusCreated, map[string]string{
		"uri": resp.Uri,
		"cid": resp.Cid,
	})
}

// handleSyncTimeline triggers a timeline sync (for Pulse scheduling or manual invocation).
func (p *Plugin) handleSyncTimeline(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	if err := p.syncTimeline(r.Context(), "manual"); err != nil {
		p.Services().Logger("atproto").Errorw("Timeline sync failed", "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Timeline sync failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "Timeline sync completed successfully",
	})
}

// handleFeedGlyph renders an AT Protocol feed as HTML.
// Query params: glyph_id (for client tracking), content (actor handle/DID), cursor (pagination)
func (p *Plugin) handleFeedGlyph(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}
	if !p.requireAuth(w) {
		return
	}

	glyphID := r.URL.Query().Get("glyph_id")
	content := r.URL.Query().Get("content") // actor handle or DID
	cursor := r.URL.Query().Get("cursor")   // pagination

	// Default to authenticated user's feed if no actor specified
	actor := content
	if actor == "" {
		actor = p.getDID()
	}

	// Fetch feed from AT Protocol
	limit := int64(20) // posts per page
	ctx := r.Context()

	var feedResp *appbsky.FeedGetAuthorFeed_Output
	if err := p.doWithRefresh(ctx, func(c *xrpc.Client) error {
		var err error
		feedResp, err = appbsky.FeedGetAuthorFeed(ctx, c, actor, cursor, "", false, limit)
		return err
	}); err != nil {
		err = errors.WithDetail(err, fmt.Sprintf("Actor: %s, cursor: %s", actor, cursor))
		p.Services().Logger("atproto").Errorw("Failed to fetch feed", "actor", actor, "error", err)
		http.Error(w, fmt.Sprintf("Failed to fetch feed: %v", err), http.StatusInternalServerError)
		return
	}

	// Render HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := p.renderFeedHTML(glyphID, actor, feedResp.Feed, feedResp.Cursor)
	w.Write([]byte(html))
}

func (p *Plugin) renderFeedHTML(glyphID, actor string, feed []*appbsky.FeedDefs_FeedViewPost, cursor *string) string {
	var html strings.Builder

	html.WriteString(fmt.Sprintf(`<div class="atproto-feed-content" data-glyph-id="%s" data-actor="%s">`, escapeHTMLAttr(glyphID), escapeHTMLAttr(actor)))

	// Header with refresh button
	html.WriteString(`<div class="feed-header">`)
	html.WriteString(fmt.Sprintf(`<div class="feed-actor">@%s</div>`, escapeHTML(actor)))
	html.WriteString(`<button class="feed-refresh" onclick="location.reload()">↻</button>`)
	html.WriteString(`</div>`)

	// Post list
	html.WriteString(`<div class="feed-posts">`)

	if len(feed) == 0 {
		html.WriteString(`<div class="feed-empty">No posts found</div>`)
	} else {
		for _, item := range feed {
			post := item.Post
			record, ok := post.Record.Val.(*appbsky.FeedPost)
			if !ok {
				continue // Skip non-post records
			}

			// Extract author info
			authorHandle := post.Author.Handle
			authorDID := post.Author.Did
			displayName := authorHandle
			if post.Author.DisplayName != nil && *post.Author.DisplayName != "" {
				displayName = *post.Author.DisplayName
			}

			// Format timestamp
			createdAt, _ := time.Parse(time.RFC3339, record.CreatedAt)
			timeAgo := formatTimeAgo(createdAt)

			// Post card
			html.WriteString(`<div class="feed-post">`)
			html.WriteString(fmt.Sprintf(`<div class="post-author">
				<span class="author-name">%s</span>
				<span class="author-handle">@%s</span>
				<span class="post-time">%s</span>
			</div>`, escapeHTML(displayName), escapeHTML(authorHandle), escapeHTML(timeAgo)))

			html.WriteString(fmt.Sprintf(`<div class="post-text">%s</div>`, escapeHTML(record.Text)))

			// Engagement metrics
			likeCount := int64(0)
			if post.LikeCount != nil {
				likeCount = *post.LikeCount
			}
			replyCount := int64(0)
			if post.ReplyCount != nil {
				replyCount = *post.ReplyCount
			}
			repostCount := int64(0)
			if post.RepostCount != nil {
				repostCount = *post.RepostCount
			}

			html.WriteString(fmt.Sprintf(`<div class="post-engagement">
				<span class="metric">❤️ %d</span>
				<span class="metric">💬 %d</span>
				<span class="metric">🔁 %d</span>
			</div>`, likeCount, replyCount, repostCount))

			// Link to open in Bluesky
			postID := extractPostID(post.Uri)
			html.WriteString(fmt.Sprintf(`<div class="post-actions">
				<a href="https://bsky.app/profile/%s/post/%s" target="_blank" class="post-link">Open in Bluesky →</a>
			</div>`, escapeHTMLAttr(authorDID), escapeHTMLAttr(postID)))

			html.WriteString(`</div>`) // end post
		}
	}

	html.WriteString(`</div>`) // end posts

	// Pagination
	if cursor != nil && *cursor != "" {
		html.WriteString(fmt.Sprintf(`<div class="feed-pagination">
			<a href="?glyph_id=%s&content=%s&cursor=%s" class="load-more">Load More</a>
		</div>`, escapeHTMLAttr(glyphID), escapeHTMLAttr(actor), escapeHTMLAttr(*cursor)))
	}

	html.WriteString(`</div>`) // end feed-content

	return html.String()
}

func formatTimeAgo(t time.Time) string {
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	} else {
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	}
}

func extractPostID(atURI string) string {
	// at://did:plc:xxx/app.bsky.feed.post/abc123 -> abc123
	parts := strings.Split(atURI, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func escapeHTMLAttr(s string) string {
	return escapeHTML(s)
}

// handleFeedGlyphCSS returns the CSS stylesheet for the feed glyph.
func (p *Plugin) handleFeedGlyphCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")

	css := `
.atproto-feed-content {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--background, #fff);
	color: var(--foreground, #000);
}

.feed-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	padding: 12px 16px;
	border-bottom: 1px solid var(--border-color, #e0e0e0);
	background: var(--header-bg, #f5f5f5);
}

.feed-actor {
	font-weight: 600;
	font-size: 14px;
}

.feed-refresh {
	background: none;
	border: 1px solid var(--border-color, #ccc);
	border-radius: 4px;
	padding: 4px 8px;
	cursor: pointer;
	font-size: 16px;
}

.feed-refresh:hover {
	background: var(--hover-bg, #e0e0e0);
}

.feed-posts {
	flex: 1;
	overflow-y: auto;
	padding: 8px;
}

.feed-empty {
	padding: 24px;
	text-align: center;
	color: var(--muted-foreground, #666);
}

.feed-post {
	background: var(--card-bg, #fff);
	border: 1px solid var(--border-color, #e0e0e0);
	border-radius: 8px;
	padding: 12px;
	margin-bottom: 8px;
}

.feed-post:hover {
	background: var(--card-hover-bg, #f9f9f9);
}

.post-author {
	display: flex;
	gap: 8px;
	align-items: baseline;
	margin-bottom: 8px;
	font-size: 14px;
}

.author-name {
	font-weight: 600;
}

.author-handle {
	color: var(--muted-foreground, #666);
}

.post-time {
	color: var(--muted-foreground, #999);
	font-size: 12px;
	margin-left: auto;
}

.post-text {
	margin-bottom: 12px;
	line-height: 1.5;
	word-break: break-word;
	overflow-wrap: break-word;
}

.post-engagement {
	display: flex;
	gap: 16px;
	margin-bottom: 8px;
	font-size: 13px;
	color: var(--muted-foreground, #666);
}

.metric {
	display: flex;
	align-items: center;
	gap: 4px;
}

.post-actions {
	display: flex;
	justify-content: flex-end;
}

.post-link {
	color: var(--link-color, #0066cc);
	text-decoration: none;
	font-size: 13px;
}

.post-link:hover {
	text-decoration: underline;
}

.feed-pagination {
	padding: 16px;
	text-align: center;
	border-top: 1px solid var(--border-color, #e0e0e0);
}

.load-more {
	display: inline-block;
	padding: 8px 16px;
	background: var(--primary, #0066cc);
	color: #fff;
	text-decoration: none;
	border-radius: 4px;
	font-weight: 500;
}

.load-more:hover {
	background: var(--primary-hover, #0052a3);
}
`

	w.Write([]byte(css))
}
