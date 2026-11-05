package agy

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/acp"
	"github.com/teranos/QNTX/internal/agentenv"
	errors "github.com/teranos/sacred-error"
)

// "correct it for libACP client use for agy"
// 「[Opus 5.5] agy is run as `agy acp`, its Agent Client Protocol server, through libACP's client (internal/acp).」

// 「[Opus 5.5] Google is agy on the node's own identity at Google Cloud: Application Default」
// 「[Opus 5.5] Credentials on an external_account file, in the node's project.」
type Google struct {
	Credentials, Project, Location string
}

// 「[Opus 5.5] Said is one turn. An empty Session starts one; otherwise agy continues it.」
// 「[Opus 5.5] It signs in as the node at Google. agy's other ACP sign-ins are a person's Google」
// 「[Opus 5.5] account, or a key somebody has to bring: "i dont want byok".」
type Said struct {
	Binary, Home, Session, Says string
	// 「[Opus 5.5] Model is the value of the session's model option, as agy offers it: gemini-3.8-flash.」
	Model string
	// 「[Opus 5.5] Google is required: agy runs as nothing else.」
	Google *Google
	// 「[Opus 5.5] Env is what the agent's turn carries beyond its own home, as its git.」
	Env []string
	// 「[Opus 5.5] Silent is told when agy has said nothing of the turn for a while, with what」
	// 「[Opus 5.5] its log says: a model answering 503 has agy retrying without a word.」
	Silent func(string)
}

type Answer struct {
	Text, Session string
	// 「[Opus 5.5] Used and Size are the context agy last reported: tokens in it, and its window.」
	Used, Size uint64
	Took       time.Duration
}

// 「[Opus 5.5] signInGoogle is the authMethods id agy's ACP server offers for Application Default Credentials: "Gemini Enterprise」
// 「[Opus 5.5] Agent Platform (formerly Vertex AI)".」
const signInGoogle = "agent-platform"

// 「[Opus 5.5] turnLog is where agy writes its log for a turn, in the agent's home: the error of a turn it ends」
// 「[Opus 5.5] without one is there and nowhere else.」
const turnLog = "agy-turn.log"

// 「[Opus 5.5] terminated is what agy says, as its message, of a turn it could not run. It ends that turn as」
// 「[Opus 5.5] end_turn all the same.」
const terminated = "Agent execution terminated due to error."

// 「[Opus 5.5] Run is one turn of agy over ACP. A turn agy reports terminated, or stopped for any reason but」
// 「[Opus 5.5] end_turn, is an error, with what agy's log says of it.」
func (s Said) Run(ctx context.Context, each func(acp.Update)) (Answer, error) {
	if s.Binary == "" || s.Home == "" || s.Model == "" || s.Google == nil || s.Says == "" {
		return Answer{}, errors.New("agy requires an explicit binary, agent home, model, the node at Google, and something said")
	}
	home, err := filepath.Abs(s.Home)
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not resolve agent home %s", s.Home)
	}
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		return Answer{}, errors.Wrapf(err, "could not create %s", work)
	}
	log := filepath.Join(home, turnLog)
	if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
		return Answer{}, errors.Wrapf(err, "could not clear %s", log)
	}
	signedIn := []string{
		"GOOGLE_APPLICATION_CREDENTIALS=" + s.Google.Credentials,
		"GOOGLE_CLOUD_PROJECT=" + s.Google.Project,
		"GOOGLE_CLOUD_LOCATION=" + s.Google.Location,
		"GOOGLE_GENAI_USE_VERTEXAI=true",
	}
	agent := acp.Agent{
		Binary: s.Binary,
		// 「[Opus 5.5] The prompt goes in over ACP, never on the command line.」
		Args: []string{"acp", "--sandbox", "--disable-telemetry", "--log-file", log},
		Dir:  work,
		Env:  append(append(append(agentenv.Carried(), s.Env...), "HOME="+home, updates), signedIn...),
	}
	turn := acp.Turn{
		Cwd: work, Session: s.Session, Auth: signInGoogle, Says: s.Says,
		Config: [][2]string{{"model", s.Model}},
		Client: "qntx", Version: PinnedVersion,
	}
	heard := func() {}
	if s.Silent != nil {
		var stop func()
		heard, stop = watchSilence(silentAfter, func() string { return logged(log) }, s.Silent)
		defer stop()
	}
	got, err := acp.Run(ctx, agent, turn, func(u acp.Update) {
		heard()
		if each != nil {
			each(u)
		}
	})
	answer := Answer{Text: got.Text, Session: got.Session, Used: got.Used, Size: got.Size, Took: got.Took}
	if err != nil {
		return answer, errors.Wrapf(err, "agy %s", PinnedVersion)
	}
	if strings.Contains(got.Text, terminated) {
		return answer, errors.Newf("agy terminated the turn: %s", logged(log))
	}
	if got.Stop != "end_turn" {
		return answer, errors.Newf("agy stopped the turn, %s: %s", got.Stop, strings.TrimSpace(got.Text))
	}
	return answer, nil
}

// 「[Opus 5.5] logged is the errors agy's log for the turn holds, the last of them last, or why there are none.」
func logged(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "its log did not open: " + err.Error()
	}
	defer f.Close()
	// 「[Opus 5.5] The end of it is the turn's end.」
	const keep = 64 * 1024
	if info, err := f.Stat(); err == nil && info.Size() > keep {
		_, _ = f.Seek(info.Size()-keep, io.SeekStart)
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return "its log did not read: " + err.Error()
	}
	var errs []string
	for _, line := range bytes.Split(raw, []byte("\n")) {
		// 「[Opus 5.5] glog's error lines: E, then the date.」
		if len(line) > 1 && line[0] == 'E' && line[1] >= '0' && line[1] <= '9' {
			errs = append(errs, string(line))
		}
	}
	if len(errs) == 0 {
		return "its log, " + path + ", names no error"
	}
	if len(errs) > 3 {
		errs = errs[len(errs)-3:]
	}
	return strings.Join(errs, "\n")
}
