package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		allowed string
		want    bool
	}{
		{"exact match", "http://localhost", "http://localhost", true},
		{"with port", "http://localhost:877", "http://localhost", true},
		{"with high port", "http://localhost:8820", "http://localhost", true},
		{"evil subdomain", "http://localhost.evil.com", "http://localhost", false},
		{"evil subdomain with port", "http://localhost.evil.com:877", "http://localhost", false},
		{"ip exact", "http://127.0.0.1", "http://127.0.0.1", true},
		{"ip with port", "http://127.0.0.1:877", "http://127.0.0.1", true},
		{"different scheme", "http://localhost", "https://localhost", false},
		{"tauri scheme", "tauri://localhost", "tauri://localhost", true},
		{"no match", "http://attacker.com", "http://localhost", false},
		{"partial host match", "http://local", "http://localhost", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchOrigin(tt.origin, tt.allowed))
		})
	}
}
