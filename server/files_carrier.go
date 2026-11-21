package server

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	errors "github.com/teranos/sacred-error"
)

// "to me it feels like k should own my Media Assets"
// "for starters when we send media to k" "i want it to be directly accesible"
// "and slowly" "it needs to kind of get out of the way of other things happening"
//
// 「[Fable 5.2] A file meant for a peer is kept here and answered for at once: who handed it over」
// 「[Fable 5.2] waits on nothing. This node then brings what it holds to that peer in the」
// 「[Fable 5.2] background, one file at a time with a pause between, and backs off while the peer」
// 「[Fable 5.2] or the primary does not answer. What is still to be brought is read from the」
// 「[Fable 5.2] node's own lines, so a restart loses none: a held line says where a file is meant」
// 「[Fable 5.2] to go, and a brought line that it is there.」

const (
	// assetMeantFor is the attribute of a held line that names the peer the file is meant for.
	assetMeantFor = "bring_to"
	// assetBrought is the line this node writes once the peer named holds the same bytes.
	assetBrought = "brought"
)

// How slowly: one file, then a pause; and how long a file that could not be
// brought waits before it is tried again, doubling up to the most.
var (
	carrierPause        = 5 * time.Second
	carrierFirstBackoff = 30 * time.Second
	carrierMostBackoff  = 10 * time.Minute
)

// toBring is one file this node keeps that a peer is to hold.
type toBring struct {
	asset    string // "sha256:<hex>"
	fileID   string // where this node keeps it
	filename string
	to       string // the peer, as the primary calls it
}

// fileCarrier is what waits to be brought, and whether the one worker runs.
type fileCarrier struct {
	mu      sync.Mutex
	waiting []toBring
	wake    chan struct{}
	running bool
}

// carry puts a file among those waiting to be brought, and starts the worker
// when it is not running. It answers at once.
func (s *QNTXServer) carry(item toBring) {
	c := &s.carrier
	c.mu.Lock()
	c.waiting = append(c.waiting, item)
	c.mu.Unlock()
	s.startCarrier()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// startCarrier starts the one worker, once. A node started with files still
// to be brought calls it too (carryWhatWaits).
func (s *QNTXServer) startCarrier() {
	c := &s.carrier
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.wake = make(chan struct{}, 1)
	c.mu.Unlock()
	s.wg.Go("files.carrier", s.carrierLoop)
}

// carryWhatWaits reads what this node still holds for a peer from its own
// lines and has the worker bring it. Called when the node has started.
func (s *QNTXServer) carryWhatWaits() {
	waiting, err := s.stillToBring()
	if err != nil {
		s.logger.Errorw("What this node still holds for a peer was not read; nothing kept before this start is brought until it is", "error", err)
		return
	}
	if len(waiting) == 0 {
		return
	}
	s.logger.Infow("Files kept here are still to be brought to a peer", "files", len(waiting))
	for _, item := range waiting {
		s.carry(item)
	}
}

// next is the first file waiting, or false.
func (c *fileCarrier) next() (toBring, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.waiting) == 0 {
		return toBring{}, false
	}
	return c.waiting[0], true
}

// done takes the first file waiting off, once it is brought or cannot be.
func (c *fileCarrier) done() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.waiting) > 0 {
		c.waiting = c.waiting[1:]
	}
}

// carrierLoop brings one file at a time until the node stops.
func (s *QNTXServer) carrierLoop() {
	c := &s.carrier
	stops := s.ctx
	if stops == nil {
		stops = context.Background()
	}
	backoff := carrierFirstBackoff
	for {
		item, waiting := c.next()
		if !waiting {
			select {
			case <-stops.Done():
				return
			case <-c.wake:
				continue
			}
		}
		wait := carrierPause
		gone, err := s.bringOne(item)
		switch {
		case err == nil:
			c.done()
			backoff = carrierFirstBackoff
		case gone:
			// Nothing here to bring any more: trying again brings nothing.
			s.logger.Errorw("A file meant for a peer is not brought: it is not here any more", "to", item.to, "asset", item.asset, "file_id", item.fileID, "error", err)
			c.done()
		default:
			s.logger.Warnw("A file meant for a peer was not brought; it is kept here and tried again", "to", item.to, "asset", item.asset, "again_in", backoff.String(), "error", err)
			wait = backoff
			if backoff *= 2; backoff > carrierMostBackoff {
				backoff = carrierMostBackoff
			}
		}
		select {
		case <-stops.Done():
			return
		case <-time.After(wait):
		}
	}
}

// bringOne brings one file this node keeps to the peer it is meant for, and
// writes that it is there. gone is whether the file is not here to bring.
func (s *QNTXServer) bringOne(item toBring) (gone bool, err error) {
	path, found, err := s.storedFile(item.fileID)
	if err != nil {
		return true, err
	}
	if !found {
		return true, errors.Newf("this node keeps no file %s", item.fileID)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, errors.Wrapf(err, "the file %s kept here did not read", item.fileID)
	}
	there, err := s.bringToPeer(item.to, item.filename, content, item.asset)
	if err != nil {
		return false, err
	}
	if err := s.attestBrought(item, there); err != nil {
		// It is there, and this node does not know: it is brought again, and
		// the peer keeps the same bytes under the same name.
		return false, err
	}
	s.logger.Infow("File brought to a peer", "to", item.to, "asset", item.asset, "file_id_there", there)
	return false, nil
}

// attestBrought writes that the peer named holds the bytes this node brought it.
func (s *QNTXServer) attestBrought(item toBring, there string) error {
	store := s.held.TheNodesOwnRecords()
	if store == nil {
		return errors.Newf("this node holds no store to attest %s into", item.asset)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", item.asset, assetBrought, item.to, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", assetBrought, item.asset)
	}
	at := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{item.asset}, Predicates: []string{assetBrought}, Contexts: []string{item.to},
		Actors: []string{s.nodeDID.DID}, Timestamp: at, CreatedAt: at, Source: pluginSource,
		Attributes: map[string]any{
			"file_id":       item.fileID,
			"file_id_there": there,
			"filename":      item.filename,
			"brought_at":    at.UTC().Format(time.RFC3339),
		},
	}); err != nil {
		return errors.Wrapf(err, "the %s line of %s was not written", assetBrought, item.asset)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}

// stillToBring is every file this node holds for a peer that no line says was
// brought there: its held lines naming a peer, less its brought lines.
func (s *QNTXServer) stillToBring() ([]toBring, error) {
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		return nil, nil
	}
	store := s.held.TheNodesOwnRecords()
	if store == nil {
		return nil, nil
	}
	everything := ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow}
	heldHere := everything
	heldHere.Predicates, heldHere.Contexts = []string{assetHeld}, []string{s.nodeDID.DID}
	held, err := store.GetAttestations(heldHere)
	if err != nil {
		return nil, errors.Wrap(err, "the held lines of this node did not read")
	}
	wasBrought := everything
	wasBrought.Predicates = []string{assetBrought}
	brought, err := store.GetAttestations(wasBrought)
	if err != nil {
		return nil, errors.Wrap(err, "the brought lines of this node did not read")
	}
	there := map[string]bool{}
	for _, line := range brought {
		if len(line.Subjects) == 1 && len(line.Contexts) == 1 {
			there[line.Subjects[0]+" "+line.Contexts[0]] = true
		}
	}
	var waiting []toBring
	for _, line := range held {
		to, _ := line.Attributes[assetMeantFor].(string)
		if to == "" || len(line.Subjects) != 1 {
			continue
		}
		asset := line.Subjects[0]
		if there[asset+" "+to] {
			continue
		}
		// The same bytes handed over twice are brought once.
		there[asset+" "+to] = true
		fileID, _ := line.Attributes["file_id"].(string)
		filename, _ := line.Attributes["filename"].(string)
		waiting = append(waiting, toBring{asset: asset, fileID: fileID, filename: filename, to: to})
	}
	return waiting, nil
}
