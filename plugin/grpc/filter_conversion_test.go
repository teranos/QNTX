package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

func TestProtoToFilterConversion(t *testing.T) {
	t.Run("convert all filter fields", func(t *testing.T) {
		now := time.Now()
		protoFilter := &protocol.AttestationFilter{
			Subjects:   []string{"user:alice", "user:bob"},
			Predicates: []string{"can:read", "can:write"},
			Contexts:   []string{"project:alpha", "env:dev"},
			Actors:     []string{"service:api", "component:auth"},
			TimeStart:  now.Unix(),
			TimeEnd:    now.Add(1 * time.Hour).Unix(),
			Limit:      100,
		}

		filter := protoToFilter(protoFilter)

		assert.Equal(t, []string{"user:alice", "user:bob"}, filter.Subjects)
		assert.Equal(t, []string{"can:read", "can:write"}, filter.Predicates)
		assert.Equal(t, []string{"project:alpha", "env:dev"}, filter.Contexts)
		assert.Equal(t, []string{"service:api", "component:auth"}, filter.Actors)
		assert.Equal(t, 100, filter.Limit)
		assert.NotNil(t, filter.TimeStart)
		assert.NotNil(t, filter.TimeEnd)
		assert.Equal(t, now.Unix(), filter.TimeStart.Unix())
		assert.Equal(t, now.Add(1*time.Hour).Unix(), filter.TimeEnd.Unix())
	})

	t.Run("backwards compatibility - single actor", func(t *testing.T) {
		protoFilter := &protocol.AttestationFilter{
			Actors: []string{"service:api"},
			Limit:  10,
		}

		filter := protoToFilter(protoFilter)

		assert.Equal(t, []string{"service:api"}, filter.Actors)
		assert.Equal(t, "service:api", filter.Actor) // Should also set single Actor field
		assert.Equal(t, 10, filter.Limit)
	})

	t.Run("empty filter fields", func(t *testing.T) {
		protoFilter := &protocol.AttestationFilter{
			Limit: 50,
		}

		filter := protoToFilter(protoFilter)

		assert.Nil(t, filter.Subjects)
		assert.Nil(t, filter.Predicates)
		assert.Nil(t, filter.Contexts)
		assert.Nil(t, filter.Actors)
		assert.Empty(t, filter.Actor)
		assert.Nil(t, filter.TimeStart)
		assert.Nil(t, filter.TimeEnd)
		assert.Equal(t, 50, filter.Limit)
	})

	t.Run("multiple actors - no single actor field", func(t *testing.T) {
		protoFilter := &protocol.AttestationFilter{
			Actors: []string{"service:api", "service:web"},
			Limit:  10,
		}

		filter := protoToFilter(protoFilter)

		assert.Equal(t, []string{"service:api", "service:web"}, filter.Actors)
		assert.Empty(t, filter.Actor) // Should NOT set single Actor field when multiple
		assert.Equal(t, 10, filter.Limit)
	})

	t.Run("time filters only", func(t *testing.T) {
		now := time.Now()
		protoFilter := &protocol.AttestationFilter{
			TimeStart: now.Unix(),
			TimeEnd:   now.Add(2 * time.Hour).Unix(),
		}

		filter := protoToFilter(protoFilter)

		assert.NotNil(t, filter.TimeStart)
		assert.NotNil(t, filter.TimeEnd)
		assert.Equal(t, now.Unix(), filter.TimeStart.Unix())
		assert.Equal(t, now.Add(2*time.Hour).Unix(), filter.TimeEnd.Unix())
	})
}

func TestFilterToProtoConversion(t *testing.T) {
	t.Run("convert all filter fields from Go to proto", func(t *testing.T) {
		now := time.Now()
		filter := ats.AttestationFilter{
			Subjects:   []string{"user:alice", "user:bob"},
			Predicates: []string{"can:read", "can:write"},
			Contexts:   []string{"project:alpha", "env:dev"},
			Actors:     []string{"service:api", "component:auth"},
			TimeStart:  &now,
			TimeEnd:    &[]time.Time{now.Add(1 * time.Hour)}[0],
			Limit:      100,
		}

		// This would be used in remote_atsstore.go
		protoFilter := &protocol.AttestationFilter{
			Subjects:   filter.Subjects,
			Predicates: filter.Predicates,
			Contexts:   filter.Contexts,
			Actors:     filter.Actors,
			TimeStart:  filter.TimeStart.Unix(),
			TimeEnd:    filter.TimeEnd.Unix(),
			Limit:      int32(filter.Limit),
		}

		assert.Equal(t, []string{"user:alice", "user:bob"}, protoFilter.Subjects)
		assert.Equal(t, []string{"can:read", "can:write"}, protoFilter.Predicates)
		assert.Equal(t, []string{"project:alpha", "env:dev"}, protoFilter.Contexts)
		assert.Equal(t, []string{"service:api", "component:auth"}, protoFilter.Actors)
		assert.Equal(t, now.Unix(), protoFilter.TimeStart)
		assert.Equal(t, now.Add(1*time.Hour).Unix(), protoFilter.TimeEnd)
		assert.Equal(t, int32(100), protoFilter.Limit)
	})

	t.Run("backwards compatibility - use single Actor field", func(t *testing.T) {
		filter := ats.AttestationFilter{
			Actor: "service:api",
			Limit: 10,
		}

		// This mimics the logic in remote_atsstore.go
		var protoActors []string
		if len(filter.Actors) > 0 {
			protoActors = filter.Actors
		} else if filter.Actor != "" {
			protoActors = []string{filter.Actor}
		}

		assert.Equal(t, []string{"service:api"}, protoActors)
	})
}