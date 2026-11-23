package auth

// "SESSION CANT REACH, IT NEEDS REACH LINE(S), IT GETS IN FRONT OF MY VIA AG OR LIVE ACTIVITY AS SOMETHING TO APPROVE,"
// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"

// SetOutOfReach has the node told of every request refused for reach.
func (h *Handler) SetOutOfReach(told func(admitted Admission, route string)) {
	h.outOfReach = told
}

// OwnDID is this node's did:key, as it signs.
func (h *Handler) OwnDID() (string, error) {
	return h.ownDID()
}
