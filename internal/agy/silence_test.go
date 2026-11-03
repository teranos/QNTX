package agy

import (
	"strings"
	"testing"
	"time"
)

// 「[Opus 5.5] A model answering 503 has agy retrying without a word, which reads as a hang.」
func TestASilentTurnIsToldWithWhatItsLogSays(t *testing.T) {
	told := make(chan string, 8)
	_, stop := watchSilence(20*time.Millisecond, func() string { return "E1102 503 UNAVAILABLE" }, func(says string) { told <- says })
	defer stop()
	select {
	case says := <-told:
		if !strings.Contains(says, "503 UNAVAILABLE") || !strings.Contains(says, "said nothing") {
			t.Fatalf("told %q", says)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a silent turn was never told")
	}
}

// 「[Opus 5.5] A turn that keeps talking is not told, and a stopped watch tells nothing more.」
func TestATalkingTurnIsNotTold(t *testing.T) {
	told := make(chan string, 8)
	heard, stop := watchSilence(200*time.Millisecond, func() string { return "" }, func(says string) { told <- says })
	until := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(until) {
		heard()
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	if len(told) != 0 {
		t.Fatalf("told %q", <-told)
	}
}
