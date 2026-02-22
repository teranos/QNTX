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

	return nil
}

// checkPaused returns true and writes 503 if plugin is paused.
func (p *Plugin) checkPaused(w http.ResponseWriter) bool {
	if p.paused {
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

	client := p.getClient()
	profile, err := appbsky.ActorGetProfile(r.Context(), client, client.Auth.Did)
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to get profile", "did", client.Auth.Did, "error", err)
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

	profile, err := appbsky.ActorGetProfile(r.Context(), p.getClient(), actor)
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to get actor profile", "actor", actor, "error", err)
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

	resp, err := appbsky.FeedGetTimeline(r.Context(), p.getClient(), "", cursor, limit)
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to get timeline", "error", err)
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

	resp, err := appbsky.FeedGetAuthorFeed(r.Context(), p.getClient(), actor, cursor, "", false, limit)
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to get author feed", "actor", actor, "error", err)
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

	resp, err := appbsky.NotificationListNotifications(r.Context(), p.getClient(), cursor, limit, false, nil, "")
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to get notifications", "error", err)
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
		config := p.services.Config("atproto")
		pdsHost := config.GetString("pds_host")
		if pdsHost == "" {
			pdsHost = "https://bsky.social"
		}
		client = &xrpc.Client{Host: pdsHost}
	}

	did, err := resolveHandle(r.Context(), client, handle)
	if err != nil {
		err = errors.WithDetail(err, fmt.Sprintf("Handle: %s", handle))
		p.services.Logger("atproto").Errorw("Failed to resolve handle", "handle", handle, "error", err)
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
	ReplyTo  string `json:"reply_to,omitempty"`  // AT URI of post to reply to
	ReplyCID string `json:"reply_cid,omitempty"` // CID of post to reply to
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

	client := p.getClient()

	post := &appbsky.FeedPost{
		Text:      req.Text,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	// Set reply reference if provided
	if req.ReplyTo != "" && req.ReplyCID != "" {
		post.Reply = &appbsky.FeedPost_ReplyRef{
			Parent: &comatproto.RepoStrongRef{
				Uri: req.ReplyTo,
				Cid: req.ReplyCID,
			},
			Root: &comatproto.RepoStrongRef{
				Uri: req.ReplyTo,
				Cid: req.ReplyCID,
			},
		}
	}

	resp, err := comatproto.RepoCreateRecord(r.Context(), client, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       client.Auth.Did,
		Record:     &util.LexiconTypeDecoder{Val: post},
	})
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to create post", "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to create post: %v", err))
		return
	}

	p.attestPost(client.Auth.Did, resp.Uri, resp.Cid, req.Text)

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

	client := p.getClient()

	follow := &appbsky.GraphFollow{
		Subject:   req.Subject,
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	resp, err := comatproto.RepoCreateRecord(r.Context(), client, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.graph.follow",
		Repo:       client.Auth.Did,
		Record:     &util.LexiconTypeDecoder{Val: follow},
	})
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to follow actor", "subject", req.Subject, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to follow %s: %v", req.Subject, err))
		return
	}

	p.attestFollow(client.Auth.Did, req.Subject, resp.Uri)

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

	client := p.getClient()

	like := &appbsky.FeedLike{
		Subject: &comatproto.RepoStrongRef{
			Uri: req.URI,
			Cid: req.CID,
		},
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	resp, err := comatproto.RepoCreateRecord(r.Context(), client, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.like",
		Repo:       client.Auth.Did,
		Record:     &util.LexiconTypeDecoder{Val: like},
	})
	if err != nil {
		p.services.Logger("atproto").Errorw("Failed to like post", "uri", req.URI, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to like post: %v", err))
		return
	}

	p.attestLike(client.Auth.Did, req.URI, resp.Uri)

	writeJSON(w, http.StatusCreated, map[string]string{
		"uri": resp.Uri,
		"cid": resp.Cid,
	})
}

