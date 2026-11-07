package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Which of a2a-go's handlers a request is handed to.

// A path that is an operation without a tenant names none; any other's first
// segment is a tenant, a namespace named after an operation among them.
func TestATenantNamedAfterAnOperationIsATenant(t *testing.T) {
	for _, asked := range []struct {
		method, path, tenant, rest string
	}{
		{http.MethodPost, "/message:send", "", "/message:send"},
		{http.MethodGet, "/tasks/abc", "", "/tasks/abc"},
		{http.MethodGet, "/extendedAgentCard", "", "/extendedAgentCard"},
		{http.MethodPost, "/garden/message:send", "garden", "/message:send"},
		{http.MethodPost, "/tasks/message:send", "tasks", "/message:send"},
		{http.MethodGet, "/extendedAgentCard/extendedAgentCard", "extendedAgentCard", "/extendedAgentCard"},
		{http.MethodPost, "/message:send/tasks/abc:cancel", "message:send", "/tasks/abc:cancel"},
	} {
		tenant, rest, named := tenantOf(asked.method, asked.path)
		assert.Equal(t, asked.tenant, tenant, asked.method+" "+asked.path)
		assert.Equal(t, asked.rest, rest, asked.method+" "+asked.path)
		assert.Equal(t, asked.tenant != "", named, asked.method+" "+asked.path)
	}
}
