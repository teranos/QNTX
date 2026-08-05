package grpc

import (
	"testing"
	"time"
)

// The cadence tracks how recently a plugin moved: fast while someone is
// releasing, slow once it has been still for a week.
func TestPollIntervalLadder(t *testing.T) {
	tests := []struct {
		sinceChange time.Duration
		want        time.Duration
	}{
		{0, 3 * time.Minute},
		{5 * time.Hour, 3 * time.Minute},
		{6 * time.Hour, 5 * time.Minute},
		{11 * time.Hour, 5 * time.Minute},
		{12 * time.Hour, 10 * time.Minute},
		{31 * time.Hour, 10 * time.Minute},
		{32 * time.Hour, time.Hour},
		{71 * time.Hour, time.Hour},
		{72 * time.Hour, time.Hour},
		{6 * 24 * time.Hour, time.Hour},
		{7 * 24 * time.Hour, 6 * time.Hour},
		{30 * 24 * time.Hour, 6 * time.Hour},
	}

	for _, tt := range tests {
		if got := pollInterval(tt.sinceChange); got != tt.want {
			t.Errorf("pollInterval(%s) = %s, want %s", tt.sinceChange, got, tt.want)
		}
	}
}

// The schedule fires at the floor, so no rung may be shorter than it — a
// plugin asking to be checked more often than the job runs would never get it.
func TestNoRungIsFasterThanTheSchedule(t *testing.T) {
	for _, rung := range updateLadder {
		if rung.every < UpdatePollInterval {
			t.Errorf("rung within %s asks for %s, faster than the %s schedule",
				rung.within, rung.every, UpdatePollInterval)
		}
	}
}

// Each rung must cover more age than the one before it, or a rung is dead code
// the loop can never reach.
func TestLadderBoundsIncrease(t *testing.T) {
	for i := 1; i < len(updateLadder); i++ {
		if updateLadder[i].within <= updateLadder[i-1].within {
			t.Errorf("rung %d bound %s does not exceed rung %d bound %s",
				i, updateLadder[i].within, i-1, updateLadder[i-1].within)
		}
	}
}
