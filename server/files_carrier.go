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

// fileCarrier is what waits to be brought, whether the one worker runs, and,
// for each peer that did not take a file, how long it is left alone.
type fileCarrier struct {
	mu        sync.Mutex
	waiting   []toBring
	wake      chan struct{}
	running   bool
	notBefore map[string]time.Time     // peer → when it is tried again
	backoff   map[string]time.Duration // peer → how long it waits after its next refusal
}

// carry puts a file among those waiting to be brought, and starts the worker
// when it is not running. It answers at once. The same bytes for the same
// peer wait once.
func (s *QNTXServer) carry(item toBring) {
	c := &s.carrier
	c.mu.Lock()
	for _, already := range c.waiting {
		if already.asset == item.asset && already.to == item.to {
			c.mu.Unlock()
			return
		}
	}
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
// lines and has the worker bring it. Called when the node has started. When
// the lines do not read, they are read again, waiting longer each time.
func (s *QNTXServer) carryWhatWaits() {
	if s.carryWhatIsRead() {
		return
	}
	s.wg.Go("files.carrier.recover", func() {
		stops := s.stopping()
		again := carrierFirstBackoff
		for {
			select {
			case <-stops.Done():
				return
			case <-time.After(again):
			}
			if s.carryWhatIsRead() {
				return
			}
			if again *= 2; again > carrierMostBackoff {
				again = carrierMostBackoff
			}
		}
	})
}

// carryWhatIsRead has the worker bring what this node's lines say it still
// holds for a peer, and says whether the lines read.
func (s *QNTXServer) carryWhatIsRead() bool {
	waiting, err := s.stillToBring()
	if err != nil {
		s.logger.Errorw("What this node still holds for a peer was not read; it is read again", "error", err)
		return false
	}
	if len(waiting) > 0 {
		s.logger.Infow("Files kept here are still to be brought to a peer", "files", len(waiting))
	}
	for _, item := range waiting {
		s.carry(item)
	}
	return true
}

// stopping is what ends when the node stops.
func (s *QNTXServer) stopping() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// next is the first file waiting whose peer is not being left alone, or, when
// every one waiting is, how long until the first of them is tried again.
func (c *fileCarrier) next(now time.Time) (item toBring, ready bool, wait time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, waiting := range c.waiting {
		at, left := c.notBefore[waiting.to]
		if !left || !now.Before(at) {
			return waiting, true, 0
		}
		if soonest := at.Sub(now); wait == 0 || soonest < wait {
			wait = soonest
		}
	}
	return toBring{}, false, wait
}

// done takes a file off those waiting, once it is brought or cannot be, and
// its peer is no longer left alone.
func (c *fileCarrier) done(item toBring) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, waiting := range c.waiting {
		if waiting.asset == item.asset && waiting.to == item.to {
			c.waiting = append(c.waiting[:i:i], c.waiting[i+1:]...)
			break
		}
	}
	delete(c.notBefore, item.to)
	delete(c.backoff, item.to)
}

// refused leaves a peer alone for longer each time it does not take a file,
// up to the most, and says for how long. Files for other peers go on.
func (c *fileCarrier) refused(to string, now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notBefore == nil {
		c.notBefore, c.backoff = map[string]time.Time{}, map[string]time.Duration{}
	}
	wait, waited := c.backoff[to]
	if !waited {
		wait = carrierFirstBackoff
	}
	c.notBefore[to] = now.Add(wait)
	next := wait * 2
	if next > carrierMostBackoff {
		next = carrierMostBackoff
	}
	c.backoff[to] = next
	return wait
}

// carrierLoop brings one file at a time until the node stops.
func (s *QNTXServer) carrierLoop() {
	c := &s.carrier
	stops := s.stopping()
	for {
		item, ready, until := c.next(time.Now())
		if !ready {
			// Nothing waits, or every peer something waits for is left alone.
			var later <-chan time.Time
			if until > 0 {
				later = time.After(until)
			}
			select {
			case <-stops.Done():
				return
			case <-c.wake:
			case <-later:
			}
			continue
		}
		gone, err := s.bringOne(item)
		switch {
		case err == nil:
			c.done(item)
		case gone:
			// Nothing here to bring any more: trying again brings nothing.
			s.logger.Errorw("A file meant for a peer is not brought: it is not here any more", "to", item.to, "asset", item.asset, "file_id", item.fileID, "error", err)
			c.done(item)
		default:
			again := c.refused(item.to, time.Now())
			s.logger.Warnw("A file meant for a peer was not brought; it is kept here and tried again", "to", item.to, "asset", item.asset, "again_in", again.String(), "error", err)
		}
		select {
		case <-stops.Done():
			return
		case <-time.After(carrierPause):
		}
	}
}

// bringOne brings one file this node keeps to the peer it is meant for, and
// writes that it is there. gone is whether the file is not here to bring. A
// file a line already says is there is not brought again.
func (s *QNTXServer) bringOne(item toBring) (gone bool, err error) {
	if there, err := s.alreadyBrought(item); err != nil {
		return false, err
	} else if there {
		return false, nil
	}
	path, found, err := s.storedFile(item.fileID)
	if err != nil {
		// Not found out is not gone: the file waits, and is brought again
		// after the peer's backoff (CodeRabbit on #1812).
		return false, err
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

// alreadyBrought is whether a line of this node says the peer holds these bytes.
func (s *QNTXServer) alreadyBrought(item toBring) (bool, error) {
	store := s.held.ReadTheNodesOwnRecords()
	if store == nil {
		return false, nil
	}
	lines, err := store.GetAttestations(ats.AttestationFilter{
		Subjects: []string{item.asset}, Predicates: []string{assetBrought}, Contexts: []string{item.to},
		TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: 1,
	})
	if err != nil {
		return false, errors.Wrapf(err, "whether %s is at %s did not read", item.asset, item.to)
	}
	return len(lines) > 0, nil
}

// stillToBring is every file this node holds for a peer that no line says was
// brought there: its held lines naming a peer, less its brought lines.
func (s *QNTXServer) stillToBring() ([]toBring, error) {
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		return nil, nil
	}
	// Read where attestHeld and attestBrought wrote, and not handed a store it could write.
	store := s.held.ReadTheNodesOwnRecords()
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
