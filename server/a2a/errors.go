package a2a

import (
	"encoding/json"
	"net/http"
)

// Error is an A2A error (§3.3.2), or a validation or routing error, as one
// binding writes it.
type Error struct {
	// Name is the A2A error type, TaskNotFoundError, or empty for an error
	// that is not one of A2A's own.
	Name    string
	Message string
	// status and grpc are what an error that is not A2A's own maps to.
	status int
	grpc   string
}

// mapped is §5.4 Error Code Mappings for the HTTP+JSON binding, with the
// reason §11.6 puts in ErrorInfo: the type in UPPER_SNAKE_CASE without
// "Error" (specification.md:1180-1190, 2910-2914).
var mapped = map[string]struct {
	status int
	grpc   string
	reason string
}{
	"TaskNotFoundError":                   {http.StatusNotFound, "NOT_FOUND", "TASK_NOT_FOUND"},
	"TaskNotCancelableError":              {http.StatusBadRequest, "FAILED_PRECONDITION", "TASK_NOT_CANCELABLE"},
	"PushNotificationNotSupportedError":   {http.StatusBadRequest, "FAILED_PRECONDITION", "PUSH_NOTIFICATION_NOT_SUPPORTED"},
	"UnsupportedOperationError":           {http.StatusBadRequest, "FAILED_PRECONDITION", "UNSUPPORTED_OPERATION"},
	"ContentTypeNotSupportedError":        {http.StatusBadRequest, "INVALID_ARGUMENT", "CONTENT_TYPE_NOT_SUPPORTED"},
	"InvalidAgentResponseError":           {http.StatusInternalServerError, "INTERNAL", "INVALID_AGENT_RESPONSE"},
	"ExtendedAgentCardNotConfiguredError": {http.StatusBadRequest, "FAILED_PRECONDITION", "EXTENDED_AGENT_CARD_NOT_CONFIGURED"},
	"ExtensionSupportRequiredError":       {http.StatusBadRequest, "FAILED_PRECONDITION", "EXTENSION_SUPPORT_REQUIRED"},
	"VersionNotSupportedError":            {http.StatusBadRequest, "FAILED_PRECONDITION", "VERSION_NOT_SUPPORTED"},
}

// Unsupported is UnsupportedOperationError: what an operation answers until it
// is one the node does.
func Unsupported(op Operation) *Error {
	return &Error{Name: "UnsupportedOperationError", Message: op.Name + " is not one this node does yet"}
}

func invalid(message string) *Error {
	return &Error{Message: message, status: http.StatusBadRequest, grpc: "INVALID_ARGUMENT"}
}

func notFound(message string) *Error {
	return &Error{Message: message, status: http.StatusNotFound, grpc: "NOT_FOUND"}
}

// write is §11.6: the google.rpc.Status JSON representation, the HTTP status
// of §5.4, and for an A2A error an ErrorInfo naming it.
func (e *Error) write(w http.ResponseWriter, undelivered Undelivered) {
	status, grpc := e.status, e.grpc
	details := []map[string]any{}
	if m, ok := mapped[e.Name]; ok {
		status, grpc = m.status, m.grpc
		details = append(details, map[string]any{
			"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
			"reason": m.reason,
			"domain": "a2a-protocol.org",
		})
	}
	if status == 0 {
		status, grpc = http.StatusInternalServerError, "INTERNAL"
	}
	body, err := json.Marshal(map[string]any{"error": map[string]any{
		"code": status, "status": grpc, "message": e.Message, "details": details,
	}})
	if err != nil {
		http.Error(w, e.Message, status)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		undelivered(e.Message, err)
	}
}
