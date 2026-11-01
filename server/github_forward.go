package server

// "if it doesnt, we need to make it do it"

// The App's webhook points at one node. A forward is ROOT's line that this node
// hands every delivery it verified to another node, untouched, as a job: written
// before the delivery is handled, kept when the far node did not answer, and
// queued again by a sweep (ADR-056).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

const (
	githubForwardLine  = "forward:"
	forwardHandlerName = "github.forward"
	// forwardUnanswered is on the error of a forward the far node never answered,
	// which is what the sweep reads a failed job for.
	forwardUnanswered = "the far node did not answer"
	// forwardWait is how long the far node has: it handles the delivery before it answers.
	forwardWait = 2 * time.Minute
	// forwardSweepEvery is how often a forward nobody answered is queued again.
	forwardSweepEvery = time.Minute
	// forwardsSwept is how many failed jobs one sweep reads.
	forwardsSwept = 1000
)

// GitHubForward is a node this node hands the App's deliveries to.
type GitHubForward struct {
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// Forwards are the forwards turned on, the newest line per URL.
func (r NodeRecords) Forwards() ([]GitHubForward, error) {
	newest, err := r.newest(githubSubject)
	if err != nil {
		return nil, err
	}
	forwards := []GitHubForward{}
	for line, as := range newest {
		if !strings.HasPrefix(line, githubForwardLine) {
			continue
		}
		if on, _ := as.Attributes["enabled"].(bool); on {
			forwards = append(forwards, GitHubForward{URL: strings.TrimPrefix(line, githubForwardLine), Enabled: true})
		}
	}
	slices.SortFunc(forwards, func(a, b GitHubForward) int { return strings.Compare(a.URL, b.URL) })
	return forwards, nil
}

// SetForward writes one forward whole.
func (r NodeRecords) SetForward(actor string, f GitHubForward) error {
	return r.nodeRecord(actor, githubSubject, githubForwardLine+f.URL, "_", map[string]any{"url": f.URL, "enabled": f.Enabled})
}

// forwardURL is whether raw names somewhere a delivery can be posted.
func forwardURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.Wrapf(err, "%q is not a URL", raw)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.Newf("%q is not an http or https URL with a host", raw)
	}
	return nil
}

// forwardPayload is one delivery as GitHub signed it, and where it goes.
type forwardPayload struct {
	URL       string `json:"url"`
	Event     string `json:"event"`
	Delivery  string `json:"delivery"`
	Signature string `json:"signature"`
	Body      []byte `json:"body"`
}

// forwardJob is the job that hands one delivery to one node. Its ID is the
// delivery's and the node's, so one delivery leaves once for each.
func forwardJob(target, event, delivery, signature string, body []byte) (*async.Job, error) {
	payload, err := json.Marshal(forwardPayload{URL: target, Event: event, Delivery: delivery, Signature: signature, Body: body})
	if err != nil {
		return nil, errors.Wrap(err, "the forward did not encode")
	}
	now := time.Now()
	return &async.Job{ID: forwardHandlerName + ":" + delivery + ":" + target, HandlerName: forwardHandlerName, Payload: payload,
		Source: "webhook", Status: async.JobStatusQueued, MadeBy: async.MadeByNoCaller, CreatedAt: now, UpdatedAt: now,
		Pulse: async.NoPulse(), Started: async.NotYet(), Completed: async.NotYet()}, nil
}

// forward queues the delivery for every node forwarded to. What goes wrong here
// is said in the log and is not this node's answer to GitHub.
func (g *gitHub) forward(event, delivery, signature string, body []byte) {
	logger := g.host.log().Named("forward")
	forwards, err := g.host.nodeRecords().Forwards()
	if err != nil {
		logger.Errorw("A delivery arrived and the forwards were not read", "event", event, "delivery", delivery, "error", err)
		return
	}
	for _, f := range forwards {
		job, err := forwardJob(f.URL, event, delivery, signature, body)
		if err == nil {
			err = g.host.enqueueForward(job)
		}
		if err != nil {
			logger.Errorw("A delivery was not queued for the node it is forwarded to", "url", f.URL, "event", event, "delivery", delivery, "error", err)
		}
	}
}

// enqueueForward queues one forward on the daemon.
func (s *QNTXServer) enqueueForward(job *async.Job) error {
	if s.daemon == nil {
		return errors.New("the node has no daemon to enqueue a forward on")
	}
	return s.daemon.GetQueue().Enqueue(job)
}

// forwardHandler posts one delivery to the node it is forwarded to.
type forwardHandler struct {
	client *http.Client
	logger *zap.SugaredLogger
}

func (h *forwardHandler) Name() string { return forwardHandlerName }

func (h *forwardHandler) Execute(ctx context.Context, job *async.Job) error {
	var p forwardPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return errors.Wrap(err, "the forward's payload did not read")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(p.Body))
	if err != nil {
		return errors.Wrapf(err, "the forward to %s is not a request", p.URL)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-GitHub-Event", p.Event)
	r.Header.Set("X-GitHub-Delivery", p.Delivery)
	r.Header.Set("X-Hub-Signature-256", p.Signature)
	answer, err := h.client.Do(r)
	if err != nil {
		h.logger.Errorw("A delivery did not reach the node it is forwarded to", "url", p.URL, "event", p.Event, "delivery", p.Delivery, "error", err)
		return errors.Wrapf(err, "%s: %s", forwardUnanswered, p.URL)
	}
	defer answer.Body.Close()
	h.logger.Infow("A delivery reached the node it is forwarded to", "url", p.URL, "event", p.Event, "delivery", p.Delivery, "status", answer.StatusCode)
	if unanswered(int64(answer.StatusCode)) {
		return errors.Newf("%s: %s gave %d", forwardUnanswered, p.URL, answer.StatusCode)
	}
	if answer.StatusCode >= http.StatusMultipleChoices {
		return errors.Newf("%s answered %s with %d", p.URL, p.Event, answer.StatusCode)
	}
	return nil
}

// sweepForwards queues again every forward the far node never answered, and
// says how many.
func sweepForwards(queue *async.Queue, logger *zap.SugaredLogger) (int, error) {
	failed, err := queue.ListJobs(async.JobStatusFailed, forwardsSwept)
	if err != nil {
		return 0, errors.Wrap(err, "the failed jobs were not read")
	}
	swept := 0
	for _, job := range failed {
		if job.HandlerName != forwardHandlerName || !strings.Contains(job.Error, forwardUnanswered) {
			continue
		}
		job.Status, job.Error, job.Completed = async.JobStatusQueued, "", async.NotYet()
		if err := queue.UpdateJob(job); err != nil {
			return swept, errors.Wrapf(err, "the forward %s was not queued again", job.ID)
		}
		swept++
	}
	if swept > 0 {
		logger.Infow("Forwards nobody answered are queued again", "forwards", swept)
	}
	return swept, nil
}

// registerForwards makes the handler known to the daemon.
func (s *QNTXServer) registerForwards() {
	if s.daemon == nil {
		return
	}
	s.daemon.Registry().Register(&forwardHandler{client: &http.Client{Timeout: forwardWait}, logger: s.logger.Named("forward")})
}

// SweepForwards runs the sweep for as long as the node does.
func (s *QNTXServer) SweepForwards() {
	if s.daemon == nil {
		return
	}
	ticker := time.NewTicker(forwardSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.lifetime().Done():
			return
		case <-ticker.C:
			if _, err := sweepForwards(s.daemon.GetQueue(), s.logger.Named("forward")); err != nil {
				s.logger.Errorw("The forwards nobody answered were not swept", "error", err)
			}
		}
	}
}

func githubForwardSigil() *protocol.Sigil {
	return &protocol.Sigil{
		Name: "forward",
		Does: "Set a node this node hands the App's webhook deliveries to, untouched, once it has verified them. The far node verifies them too, so it holds this node's webhook secret. Turned off, nothing goes there.",
		Takes: []*protocol.Param{
			{Name: "url", Required: true, Says: "The far node's webhook URL."},
			{Name: "enabled", Required: true, Says: "true or false."},
		},
		Gives: []*protocol.Field{{Name: "forward", Says: "The forward as it now stands."}},
		Http:  &protocol.Endpoint{Method: http.MethodPost, Path: githubPath + "/forward"},
	}
}

func (g *gitHub) githubForward(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	on, err := strconv.ParseBool(sent["enabled"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "enabled", Says: "enabled is true or false, and this said " + sent["enabled"]}
	}
	if err := forwardURL(sent["url"]); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "url", Says: err.Error()}
	}
	f := GitHubForward{URL: sent["url"], Enabled: on}
	if err := g.host.nodeRecords().SetForward(actorOf(ctx), f); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]GitHubForward{"forward": f}, nil
}
