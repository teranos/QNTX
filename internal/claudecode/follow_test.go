package claudecode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

	// The turn's process is gone by now, as Claude Code is once it printed its last result.
	ended := exec.Command("true")
	if err := ended.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stream+".pid", []byte(strconv.Itoa(ended.Process.Pid)), 0o600); err != nil {
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

// "i actually think the node agent got it, i can see it now. but weirdly enough, it did not respond"
// A resumed session answers what was queued before what was said: background
// tasks that ended between turns are a prompt of their own, with a result of
// their own. The turn's answer is its last result, read once the process is gone.
func TestATurnIsAnsweredByItsLastResult(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(turnsDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	stream := filepath.Join(turnsDir(home), "s-4.1791900000000000000"+streamExt)
	notice := `{"type":"result","subtype":"success","session_id":"s-4","is_error":false,"result":""}` + "\n"
	if err := os.WriteFile(stream, []byte(notice), 0o600); err != nil {
		t.Fatal(err)
	}
	printing := exec.Command("sleep", "2")
	if err := printing.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stream+".pid", []byte(strconv.Itoa(printing.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(5 * followEvery)
		appended, err := os.OpenFile(stream, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		_, _ = appended.WriteString(`{"type":"result","subtype":"success","session_id":"s-4","is_error":false,"result":"#1594 merged."}` + "\n")
		_ = appended.Close()
		_ = printing.Wait()
	}()

	answer, err := Turn{Session: "s-4", Stream: stream}.Follow(context.Background(), func(Message) {})
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if answer.Text != "#1594 merged." {
		t.Fatalf("the turn was answered with %q, and not with what was said last", answer.Text)
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

// A second reader tails the stream while the turn prints to it: each line is
// handed over as it lands, before the result exists, and the .read mark the
// turn's own follow keeps is not moved.
func TestTailHandsEachLineAsItLandsAndMovesNoMark(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(turnsDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	stream := filepath.Join(turnsDir(home), "s-2.1791900000000000000"+streamExt)
	init := `{"type":"system","subtype":"init","session_id":"s-2","model":"m"}` + "\n"
	if err := os.WriteFile(stream, []byte(init), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stream+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stream+".read", []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	appended, err := os.OpenFile(stream, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer appended.Close()

	landed := make(chan string, 8)
	tailed := make(chan struct{})
	var end int64
	var live bool
	go func() {
		defer close(tailed)
		end, live, err = Tail(context.Background(), stream, 0, func(line []byte, _ int64) { landed <- string(line) })
	}()
	if got := <-landed; got != init {
		t.Fatalf("the first line handed over is %q", got)
	}
	delta := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Up"}}}` + "\n"
	if _, err := appended.WriteString(delta); err != nil {
		t.Fatal(err)
	}
	// The delta lands while no result exists in the file: that is the turn
	// still printing, and the reader already holding its words.
	select {
	case got := <-landed:
		if got != delta {
			t.Fatalf("the line handed over is %q", got)
		}
	case <-time.After(20 * followEvery):
		t.Fatal("the delta was not handed over while the turn printed")
	}
	result := `{"type":"result","subtype":"success","session_id":"s-2","is_error":false,"result":"Up 3 days."}` + "\n"
	if _, err := appended.WriteString(result); err != nil {
		t.Fatal(err)
	}
	if got := <-landed; got != result {
		t.Fatalf("the last line handed over is %q", got)
	}
	<-tailed
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if live {
		t.Error("the turn is said to print on after its result")
	}
	if want := int64(len(init) + len(delta) + len(result)); end != want {
		t.Errorf("the end is %d, and the stream holds %d bytes", end, want)
	}
	mark, err := os.ReadFile(stream + ".read")
	if err != nil {
		t.Fatal(err)
	}
	if string(mark) != "0" {
		t.Errorf("the tail moved the turn's own mark to %s", mark)
	}
}

// A turn that runs where no process prints a stream, an agent at Anthropic,
// is mirrored by the node into one, and a tail reads it as any turn's.
func TestAMirroredTurnIsTailedAsAnyTurnIs(t *testing.T) {
	home := t.TempDir()
	mirror, err := OpenMirror(home, "s-3")
	if err != nil {
		t.Fatal(err)
	}
	landed := make(chan string, 8)
	tailed := make(chan struct{})
	var live bool
	go func() {
		defer close(tailed)
		_, live, err = Tail(context.Background(), mirror.Stream, 0, func(line []byte, _ int64) { landed <- string(line) })
	}()
	if err := mirror.Print(TextMessage("Shaken.")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-landed:
		if !strings.Contains(got, `"text":"Shaken."`) {
			t.Fatalf("the line handed over is %q", got)
		}
	case <-time.After(20 * followEvery):
		t.Fatal("the mirrored line was not handed over while the turn ran")
	}
	if err := mirror.Print(Message{Type: "result", Subtype: "success", Result: "Shaken."}); err != nil {
		t.Fatal(err)
	}
	<-landed
	<-tailed
	if err != nil || live {
		t.Fatalf("Tail: err %v live %v", err, live)
	}
	if err := mirror.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mirror.Stream); !os.IsNotExist(err) {
		t.Errorf("the mirrored stream was not removed when the turn ended")
	}
}
