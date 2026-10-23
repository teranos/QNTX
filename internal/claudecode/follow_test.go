package claudecode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	errors "github.com/teranos/sacred-error"
)

// A node that stopped reading a turn leaves its stream where it was, and the
// next node reads it on from there to its result.
func TestATurnTheNodeStoppedReadingIsReadOnByTheNext(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(turnsDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	stream := filepath.Join(turnsDir(home), "s-1.1791900000000000000"+streamExt)
	first := `{"type":"system","subtype":"init","session_id":"s-1","model":"m","claude_code_version":"2.1.289"}` + "\n"
	if err := os.WriteFile(stream, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stream+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}

	stopping, stop := context.WithCancel(context.Background())
	var firstRead []string
	go func() {
		time.Sleep(3 * followEvery)
		stop()
	}()
	answer, err := Turn{Session: "s-1", Stream: stream}.Follow(stopping, func(m Message) { firstRead = append(firstRead, m.Type) })
	if err == nil {
		t.Fatalf("a node that stopped reading took %+v as the answer", answer)
	}
	if len(firstRead) != 1 {
		t.Fatalf("the first node read %v", firstRead)
	}

	pending, err := Pending(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Session != "s-1" {
		t.Fatalf("pending is %+v", pending)
	}
	result := `{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"done"}` + "\n"
	appended, err := os.OpenFile(stream, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appended.WriteString(result); err != nil {
		t.Fatal(err)
	}
	if err := appended.Close(); err != nil {
		t.Fatal(err)
	}

	var secondRead []string
	answer, err = pending[0].Follow(context.Background(), func(m Message) { secondRead = append(secondRead, m.Type) })
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if answer.Text != "done" {
		t.Errorf("the answer is %+v", answer)
	}
	if len(secondRead) != 1 || secondRead[0] != "result" {
		t.Errorf("the next node read %v, and not only what the first had not", secondRead)
	}
	left, err := Pending(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("a turn read to its end is still pending: %+v", left)
	}
}

// A turn is interrupted the way a person stops it, its whole process group
// when it leads one, and one already gone is no error.
func TestATurnIsInterruptedTheWayAPersonStopsIt(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(turnsDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	name := "s.1" + streamExt
	if err := Interrupt(home, name); err != nil {
		t.Fatalf("a turn with no process was not left be: %v", err)
	}

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(turnsDir(home), name+".pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Interrupt(home, name); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.Sys().(syscall.WaitStatus).Signal() != syscall.SIGINT {
		t.Fatalf("the turn did not end on an interrupt: %v", err)
	}
	if err := Interrupt(home, name); err != nil {
		t.Fatalf("a turn already gone was an error: %v", err)
	}
}
