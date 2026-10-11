package server

// A delivery that reached nobody is not lost: GitHub keeps the App webhook's
// deliveries, and after every start the node asks for the ones it never
// answered and has GitHub send them again, through the one webhook.

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// deliveriesAsker is the part of GitHubService a catch-up spends.
type deliveriesAsker interface {
	ListDeliveriesForAnAppWebhook(context.Context, *protocol.GitHubListDeliveriesForAnAppWebhookRequest) (*protocol.GitHubListDeliveriesForAnAppWebhookResponse, error)
	RedeliverADeliveryForAnAppWebhook(context.Context, *protocol.GitHubRedeliverADeliveryForAnAppWebhookRequest) (*protocol.GitHubRedeliverADeliveryForAnAppWebhookResponse, error)
}

// deliveriesPage is the most GitHub gives in one page.
const deliveriesPage = 100

// caughtUpFile is beside the node's db: when the last catch-up began.
const caughtUpFile = "github-deliveries.caught-up"

// unanswered is a status GitHub records when the node did not answer: no
// connection at all, or Caddy with nothing behind it. A status the node gave
// itself, an error included, is an answer, and sending it again changes nothing.
func unanswered(code int64) bool {
	switch code {
	case 0, 502, 503, 504:
		return true
	}
	return false
}

// catchUpDeliveries pages through the deliveries since the last catch-up,
// newest first, and redelivers each event the node never answered, once, by
// its newest delivery. It returns the deliveries it had sent again.
func catchUpDeliveries(ctx context.Context, gh deliveriesAsker, since time.Time, logger *zap.SugaredLogger) ([]int64, error) {
	answered := map[string]bool{}
	newest := map[string]int64{}
	var order []string

	cursor := ""
	for {
		page, err := gh.ListDeliveriesForAnAppWebhook(ctx, &protocol.GitHubListDeliveriesForAnAppWebhookRequest{PerPage: deliveriesPage, Cursor: cursor})
		if err != nil {
			return nil, errors.Wrap(err, "the App webhook's deliveries were not listed")
		}
		if !page.Success {
			return nil, errors.Newf("the App webhook's deliveries were not listed: %s", page.Error)
		}
		reachedSince := false
		for _, item := range page.Items {
			at, err := time.Parse(time.RFC3339, item.DeliveredAt)
			if err != nil {
				return nil, errors.Wrapf(err, "delivery %d says it was delivered at %q", item.Id, item.DeliveredAt)
			}
			if at.Before(since) {
				reachedSince = true
				continue
			}
			if _, seen := newest[item.Guid]; !seen {
				newest[item.Guid] = item.Id
				order = append(order, item.Guid)
			}
			if !unanswered(item.StatusCode) {
				answered[item.Guid] = true
			}
		}
		cursor = nextCursor(page.Link)
		if reachedSince || cursor == "" {
			break
		}
	}

	var redelivered []int64
	for _, guid := range order {
		if answered[guid] {
			continue
		}
		id := newest[guid]
		said, err := gh.RedeliverADeliveryForAnAppWebhook(ctx, &protocol.GitHubRedeliverADeliveryForAnAppWebhookRequest{DeliveryId: id})
		if err == nil && !said.Success {
			err = errors.New(said.Error)
		}
		if err != nil {
			return redelivered, errors.Wrapf(err, "delivery %d of event %s was not sent again", id, guid)
		}
		logger.Infow("A delivery the node never answered is sent again", "delivery", id, "guid", guid)
		redelivered = append(redelivered, id)
	}
	return redelivered, nil
}

// nextCursor is the cursor of the next page in a Link header, or empty on the last.
func nextCursor(link string) string {
	for _, part := range strings.Split(link, ",") {
		target, rel, ok := strings.Cut(part, ";")
		if !ok || strings.TrimSpace(rel) != `rel="next"` {
			continue
		}
		u, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			return ""
		}
		return u.Query().Get("cursor")
	}
	return ""
}

// CatchUpDeliveries sends again what GitHub could not deliver while the node
// was down. The mark moves only when the whole catch-up went through, so one
// that failed is tried over the same window at the next start.
func (s *QNTXServer) CatchUpDeliveries() {
	logger := s.logger.Named("github")
	if _, active := s.gitHubWebhook(); !active {
		return
	}
	if _, err := (gitHubCredentials{s: s}).AppToken(); err != nil {
		logger.Infow("The App webhook's deliveries are not caught up", "why", err)
		return
	}
	mark := filepath.Join(filepath.Dir(s.dbPath), caughtUpFile)
	var since time.Time
	if kept, err := os.ReadFile(mark); err == nil {
		if since, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(string(kept))); err != nil {
			logger.Errorw("The last catch-up's mark does not read, so every delivery GitHub keeps is looked at", "mark", mark, "error", err)
		}
	} else if !os.IsNotExist(err) {
		logger.Errorw("The last catch-up's mark was not read, so every delivery GitHub keeps is looked at", "mark", mark, "error", err)
	}

	began := time.Now().UTC()
	redelivered, err := catchUpDeliveries(s.lifetime(), s.gitHubService(), since, logger)
	if err != nil {
		logger.Errorw("The App webhook's deliveries were not caught up", "since", since, "redelivered", len(redelivered), "error", err)
		return
	}
	if err := os.WriteFile(mark, []byte(began.Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		logger.Errorw("The App webhook's deliveries were caught up and the mark was not kept, so the next start looks again", "mark", mark, "error", err)
	}
	logger.Infow("The App webhook's deliveries are caught up", "since", since, "redelivered", len(redelivered))
}
