package server

import "strings"

// "A mail actually goes out to both me and my friend"

// wireInvitations hands the auth handler the node's mail and the page an
// invitation's links open on: the first rp_origin, as a canvas invitation's.
func (s *QNTXServer) wireInvitations() {
	if s.authHandler == nil || s.nodeMailer == nil {
		s.logger.Warnw("Invitations send no mail", "has_auth", s.authHandler != nil, "has_mail", s.nodeMailer != nil)
		return
	}
	page := ""
	if origins := s.deps.cfg.Auth.RPOrigins; len(origins) > 0 {
		page = strings.TrimSuffix(origins[0], "/")
	}
	s.authHandler.SetInvitationMail(s.nodeMailer, page)
	s.logger.Infow("Invitations wired", "page", page)
}
