//go:build libacp

package acp

/*
#cgo CFLAGS: -I${SRCDIR}/../../target/libacp/include
#cgo LDFLAGS: -L${SRCDIR}/../../target/libacp/lib -lacp -ljson-c
#include <stdlib.h>
#include "acp.h"
*/
import "C"

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	errors "github.com/teranos/sacred-error"
)

// Linked is whether this QNTX was built with libACP, and so runs an ACP harness.
const Linked = true

// The agent may write a line this long: a tool's output, a file it read.
const maxLine = 16 * 1024 * 1024

// stopReasons is PromptResponse's stopReason, in libACP's acp_stop_reason order.
var stopReasons = [...]string{"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled"}

// Run starts the agent, signs in when Auth names a method, starts or continues the session, sets its
// options, says Says, and hands each update to each as it comes. It answers with the agent's message and
// why the turn stopped, or the error that broke it: the agent's own, with what it wrote on stderr when
// it ended before answering.
func Run(ctx context.Context, a Agent, t Turn, each func(Update)) (Answer, error) {
	if a.Binary == "" || t.Says == "" || !filepath.IsAbs(t.Cwd) {
		return Answer{}, errNoTurn
	}
	if each == nil {
		each = func(Update) {}
	}
	if t.Permission == nil {
		t.Permission = rejected
	}
	cmd := exec.CommandContext(ctx, a.Binary, a.Args...)
	cmd.Dir, cmd.Env = a.Dir, a.Env
	cmd.WaitDelay = time.Second
	var stderr tail
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not write to %s", a.Binary)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not read %s", a.Binary)
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return Answer{}, errors.Wrapf(err, "could not start %s", a.Binary)
	}
	c := &conn{ctx: ctx, in: in, lines: make(chan []byte), each: each, permission: t.Permission}
	c.read(out)
	answer, err := c.turn(t)
	answer.Took = time.Since(started)
	// The agent ends when its input does.
	_ = in.Close()
	if err != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		// What the agent wrote on stderr is its side of why.
		if said := stderr.String(); said != "" {
			return answer, errors.Wrapf(err, "%s said: %s", filepath.Base(a.Binary), said)
		}
		return answer, err
	}
	if waitErr != nil {
		return answer, errors.Wrapf(waitErr, "%s answered and then failed: %s", filepath.Base(a.Binary), stderr.String())
	}
	return answer, nil
}

type conn struct {
	ctx        context.Context
	in         io.Writer
	lines      chan []byte
	readErr    error
	next       int
	each       func(Update)
	permission func(Asked) string
	// 「[Opus 5.5] quiet holds back the updates a session told again replays, which the caller was told before.」
	quiet bool
	// answer is what the turn's updates said so far.
	answer *Answer
}

// read hands each line the agent writes to lines, closing it when the agent's output ends.
func (c *conn) read(out io.Reader) {
	go func() {
		defer close(c.lines)
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 64*1024), maxLine)
		for s.Scan() {
			line := append([]byte(nil), s.Bytes()...)
			if len(strings.TrimSpace(string(line))) == 0 {
				continue
			}
			select {
			case c.lines <- line:
			case <-c.ctx.Done():
				return
			}
		}
		c.readErr = s.Err()
	}()
}

func (c *conn) turn(t Turn) (Answer, error) {
	answer := Answer{}
	c.answer = &answer
	client, version := t.Client, t.Version
	if client == "" {
		client = "qntx"
	}
	if version == "" {
		version = "dev"
	}

	offer := C.acp_client_capabilities{}
	line, err := c.call("initialize", func(id *C.char, m *C.acp_buffer) C.acp_status {
		name, ver := C.CString(client), C.CString(version)
		defer C.free(unsafe.Pointer(name))
		defer C.free(unsafe.Pointer(ver))
		return C.acp_client_initialize_write(id, 1, &offer, name, ver, m)
	})
	if err != nil {
		return answer, err
	}
	var info C.acp_agent_info
	withLine(line, func(p *C.char, n C.size_t) { C.acp_client_initialize_read(p, n, &info) })
	answer.Agent, answer.AgentVersion = goString(info.agent_name), goString(info.agent_version)
	speaks, loads, resumes := uint(info.protocol_version), info.load_session != 0, info.session_resume != 0
	C.acp_agent_info_free(&info)
	if speaks != 1 {
		return answer, errors.Newf("%s speaks ACP %d, and libACP's client speaks 1", answer.Agent, speaks)
	}

	if t.Auth != "" {
		if _, err := c.call("authenticate", func(id *C.char, m *C.acp_buffer) C.acp_status {
			method := C.CString(t.Auth)
			defer C.free(unsafe.Pointer(method))
			return C.acp_client_authenticate_write(id, method, m)
		}); err != nil {
			return answer, err
		}
	}

	cwd := C.CString(t.Cwd)
	defer C.free(unsafe.Pointer(cwd))
	resumed := t.Session != ""
	switch {
	case !resumed:
		line, err := c.call("session/new", func(id *C.char, m *C.acp_buffer) C.acp_status {
			return C.acp_client_session_new_write(id, cwd, nil, 0, m)
		})
		if err != nil {
			return answer, err
		}
		var sid *C.char
		withLine(line, func(p *C.char, n C.size_t) { C.acp_client_session_new_read(p, n, &sid) })
		answer.Session = goString(sid)
		C.acp_session_id_free(&sid)
		if answer.Session == "" {
			return answer, errors.Newf("%s answered session/new with no sessionId: %s", answer.Agent, line)
		}
	case resumes || loads:
		sid := C.CString(t.Session)
		defer C.free(unsafe.Pointer(sid))
		method := "session/resume"
		write := func(id *C.char, m *C.acp_buffer) C.acp_status {
			return C.acp_client_session_resume_write(id, sid, cwd, nil, 0, m)
		}
		// 「[Opus 5.5] Continuing tells the session again as updates, session/load always and agy's session/resume」
		// 「[Opus 5.5] too; the caller has them already, and they are not this turn's answer.」
		c.quiet = true
		if !resumes {
			method = "session/load"
			write = func(id *C.char, m *C.acp_buffer) C.acp_status {
				return C.acp_client_session_load_write(id, sid, cwd, nil, 0, m)
			}
		}
		_, err := c.call(method, write)
		c.quiet = false
		if err != nil {
			return answer, err
		}
		answer.Session = t.Session
	default:
		return answer, errors.Newf("%s continues no session: it offers neither session/resume nor session/load", answer.Agent)
	}
	c.each(Update{Kind: Started, Session: answer.Session, Resumed: resumed})

	sid := C.CString(answer.Session)
	defer C.free(unsafe.Pointer(sid))
	for _, option := range t.Config {
		if _, err := c.call("session/set_config_option", func(id *C.char, m *C.acp_buffer) C.acp_status {
			config, value := C.CString(option[0]), C.CString(option[1])
			defer C.free(unsafe.Pointer(config))
			defer C.free(unsafe.Pointer(value))
			return C.acp_client_session_set_config_option_write(id, sid, config, value, m)
		}); err != nil {
			return answer, errors.Wrapf(err, "%s was not set to %s", option[0], option[1])
		}
	}

	line, err = c.call("session/prompt", func(id *C.char, m *C.acp_buffer) C.acp_status {
		says := C.CString(t.Says)
		defer C.free(unsafe.Pointer(says))
		return C.acp_client_session_prompt_write(id, sid, says, C.size_t(len(t.Says)), m)
	})
	if err != nil {
		// A turn left is called off, so the agent stops what it was doing for it.
		var m C.acp_buffer
		if C.acp_client_session_cancel_write(sid, &m) == C.ACP_OK {
			_ = c.send(takeBuffer(&m))
		}
		return answer, err
	}
	var stopped C.int
	var reason C.acp_stop_reason
	withLine(line, func(p *C.char, n C.size_t) { C.acp_client_session_prompt_read(p, n, &stopped, &reason) })
	if stopped == 0 || int(reason) >= len(stopReasons) {
		return answer, errors.Newf("%s answered session/prompt with no stopReason: %s", answer.Agent, line)
	}
	answer.Stop = stopReasons[reason]
	return answer, nil
}

// call writes a request and reads until its answer, telling each update and answering each request of
// the agent's on the way. The answer's line, or the agent's error as an *Error.
func (c *conn) call(method string, write func(id *C.char, m *C.acp_buffer) C.acp_status) ([]byte, error) {
	c.next++
	id := strconv.Itoa(c.next)
	cid := C.CString(id)
	defer C.free(unsafe.Pointer(cid))
	var m C.acp_buffer
	if status := write(cid, &m); status != C.ACP_OK {
		return nil, errors.Newf("libACP did not write %s: %s", method, C.GoString(C.acp_status_string(status)))
	}
	if err := c.send(takeBuffer(&m)); err != nil {
		return nil, errors.Wrapf(err, "%s was not sent", method)
	}
	for {
		var line []byte
		var open bool
		select {
		case line, open = <-c.lines:
		case <-c.ctx.Done():
			return nil, errors.Wrapf(c.ctx.Err(), "the turn was left waiting on %s", method)
		}
		if !open {
			if c.readErr != nil {
				return nil, errors.Wrapf(c.readErr, "the agent's output broke off before it answered %s", method)
			}
			return nil, errors.Newf("the agent ended before it answered %s", method)
		}
		var msg C.acp_message
		var status C.acp_status
		withLine(line, func(p *C.char, n C.size_t) { status = C.acp_client_message_read(p, n, &msg) })
		if status != C.ACP_OK {
			return nil, errors.Newf("the agent wrote a line that is not JSON-RPC while %s was asked: %s", method, line)
		}
		kind, msgID, msgMethod, params := msg.kind, goString(msg.id), goString(msg.method), goString(msg.params)
		isErr, code, message, data := msg.is_error != 0, int(msg.error_code), goString(msg.error_message), goString(msg.error_data)
		C.acp_message_free(&msg)
		switch kind {
		case C.ACP_MESSAGE_RESPONSE:
			if msgID != id {
				continue
			}
			if isErr {
				return line, &Error{Method: method, Code: code, Message: message, Data: data}
			}
			return line, nil
		case C.ACP_MESSAGE_NOTIFICATION:
			if msgMethod == "session/update" && !c.quiet {
				c.update(line)
			}
		case C.ACP_MESSAGE_REQUEST:
			if err := c.answerRequest(msgID, msgMethod, params); err != nil {
				return nil, err
			}
		}
	}
}

func (c *conn) update(line []byte) {
	var u C.acp_update
	withLine(line, func(p *C.char, n C.size_t) { C.acp_client_session_update_read(p, n, &u) })
	defer C.acp_update_free(&u)
	if u.kind == C.ACP_UPDATE_UNREAD {
		return
	}
	kinds := map[C.acp_update_kind]UpdateKind{
		C.ACP_UPDATE_OTHER: Other, C.ACP_UPDATE_USER_MESSAGE: UserMessage, C.ACP_UPDATE_AGENT_MESSAGE: AgentMessage,
		C.ACP_UPDATE_AGENT_THOUGHT: AgentThought, C.ACP_UPDATE_TOOL_CALL: ToolCall,
		C.ACP_UPDATE_TOOL_CALL_UPDATE: ToolCallUpdate, C.ACP_UPDATE_USAGE: Usage,
	}
	text := ""
	if u.text != nil {
		text = C.GoStringN(u.text, C.int(u.text_length))
	}
	out := Update{
		Kind: kinds[u.kind], Session: goString(u.session_id), Type: goString(u._type), Raw: goString(u.update), Text: text,
		ToolCallID: goString(u.tool_call_id), Title: goString(u.title), Name: goString(u.name), ToolKind: goString(u.tool_kind),
		Status: goString(u.status), RawInput: goString(u.raw_input), RawOutput: goString(u.raw_output),
		Used: uint64(u.used), Size: uint64(u.size), CostAmount: goString(u.cost_amount), CostCurrency: goString(u.cost_currency),
	}
	switch out.Kind {
	case AgentMessage:
		c.answer.Text += out.Text
	case Usage:
		c.answer.Used, c.answer.Size = out.Used, out.Size
	}
	c.each(out)
}

// answerRequest answers what the agent asks: a permission by the turn's choice, and anything else, which
// this client offers none of, with -32601.
func (c *conn) answerRequest(id, method, params string) error {
	cid := C.CString(id)
	defer C.free(unsafe.Pointer(cid))
	var m C.acp_buffer
	status := C.acp_status(C.ACP_OK)
	if method == "session/request_permission" {
		var asked C.acp_permission_asked
		withLine([]byte(params), func(p *C.char, n C.size_t) { status = C.acp_client_session_request_permission_read(p, n, &asked) })
		if status != C.ACP_OK {
			why := C.CString("session/request_permission names no sessionId, toolCall or options")
			defer C.free(unsafe.Pointer(why))
			status = C.acp_client_error_write(cid, C.ACP_ERROR_INVALID_PARAMS, why, &m)
		} else {
			a := Asked{Session: goString(asked.session_id), ToolCallID: goString(asked.tool_call_id), Title: goString(asked.title),
				ToolKind: goString(asked.tool_kind), RawInput: goString(asked.raw_input)}
			for _, o := range unsafe.Slice(asked.options, asked.count) {
				a.Options = append(a.Options, Option{ID: goString(o.option_id), Name: goString(o.name), Kind: goString(o.kind)})
			}
			C.acp_permission_asked_free(&asked)
			var chosen *C.char
			if option := c.permission(a); option != "" {
				chosen = C.CString(option)
				defer C.free(unsafe.Pointer(chosen))
			}
			status = C.acp_client_session_request_permission_write(cid, chosen, &m)
		}
	} else {
		why := C.CString("this client offers no " + method)
		defer C.free(unsafe.Pointer(why))
		status = C.acp_client_error_write(cid, -32601, why, &m)
	}
	if status != C.ACP_OK {
		return errors.Newf("libACP did not write the answer to %s: %s", method, C.GoString(C.acp_status_string(status)))
	}
	return errors.Wrapf(c.send(takeBuffer(&m)), "the answer to %s was not sent", method)
}

func (c *conn) send(b []byte) error {
	_, err := c.in.Write(append(b, '\n'))
	return err
}

// withLine hands a line to libACP as the bytes it is, for as long as f runs.
func withLine(line []byte, f func(*C.char, C.size_t)) {
	if len(line) == 0 {
		var none [1]byte
		f((*C.char)(unsafe.Pointer(&none[0])), 0)
		return
	}
	f((*C.char)(unsafe.Pointer(&line[0])), C.size_t(len(line)))
}

func takeBuffer(m *C.acp_buffer) []byte {
	defer C.acp_buffer_free(m)
	return C.GoBytes(unsafe.Pointer(m.data), C.int(m.length))
}

func goString(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

// tail keeps the last of what the agent wrote on stderr, bounded while it runs.
type tail struct{ data []byte }

func (b *tail) Write(p []byte) (int, error) {
	const limit = 4096
	b.data = append(b.data, p...)
	if len(b.data) > limit {
		b.data = b.data[len(b.data)-limit:]
	}
	return len(p), nil
}

func (b *tail) String() string { return strings.TrimSpace(string(b.data)) }
