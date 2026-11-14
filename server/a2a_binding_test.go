package server

import (
	"net/http"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		v, path, spoken := spokenAt("/a2a" + asked.path)
		require.True(t, spoken, asked.path)
		assert.Equal(t, a2asdk.Version, v.version, asked.path)
		tenant, rest, named := v.tenantAt(asked.method, path)
		assert.Equal(t, asked.tenant, tenant, asked.method+" "+asked.path)
		assert.Equal(t, asked.rest, rest, asked.method+" "+asked.path)
		assert.Equal(t, asked.tenant != "", named, asked.method+" "+asked.path)
	}
}

// Where a request arrives is the version it is in: 0.3 beneath /a2a/0.3, its
// operations under /v1 and its tenant the segment before them, and 1.0
// everywhere else beneath /a2a.
func TestThePathIsTheVersion(t *testing.T) {
	for _, asked := range []struct {
		method, path, version, tenant, rest string
	}{
		{http.MethodPost, "/a2a", "1.0", "", "/"},
		{http.MethodPost, "/a2a/message:send", "1.0", "", "/message:send"},
		{http.MethodPost, "/a2a/0.3", "0.3", "", "/"},
		{http.MethodPost, "/a2a/0.3/v1/message:send", "0.3", "", "/v1/message:send"},
		{http.MethodGet, "/a2a/0.3/v1/card", "0.3", "", "/v1/card"},
		{http.MethodPost, "/a2a/0.3/garden", "0.3", "garden", "/"},
		{http.MethodPost, "/a2a/0.3/garden/v1/message:send", "0.3", "garden", "/v1/message:send"},
	} {
		v, path, spoken := spokenAt(asked.path)
		require.True(t, spoken, asked.path)
		assert.Equal(t, asked.version, string(v.version), asked.path)
		tenant, rest, _ := v.tenantAt(asked.method, path)
		assert.Equal(t, asked.tenant, tenant, asked.path)
		assert.Equal(t, asked.rest, rest, asked.path)
	}
	_, _, spoken := spokenAt("/a2ab")
	assert.False(t, spoken, "/a2ab is beneath no version's base")
}
