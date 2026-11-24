package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/ai/provider"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	errors "github.com/teranos/sacred-error"
)

// "over time i want to move everything over, and have a read on everything,"
// "but i realise it could take years though."
// "despite that, i think we can definitely do like 10/20 per night or so."
// "newest first because,"
// "there's going to be more than 20, think more like 10K+ eventually"
//
// "and the 02:00 run idea can be removed if gaze is load based"
//
// 「[Opus 5.5] gaze yields: it runs at the lowest priority there is, and the node takes it out of」
// 「[Opus 5.5] the way while memory is short (plugin_yield.go). So the node reads whenever a」
// 「[Opus 5.5] picture waits: every readEvery, the newest it holds that have no reading yet, with」
// 「[Opus 5.5] the provider files.read_with names, one at a time, and keeps each reading as a line」
// 「[Opus 5.5] on its picture. A picture whose reading failed has none, so the next round tries it」
// 「[Opus 5.5] again. A node that names no provider reads nothing.」

const (
	readingHandlerName = "files.read"
	// assetRead is the line that holds what was read in a picture.
	assetRead = "read"
	// readEvery is how often the node looks for pictures to read.
	readEvery = 5 * time.Minute
	// readPerRound is the most pictures read in one round; the rest wait for the next.
	readPerRound = 20
	// readPause is the pause between one picture and the next.
	readPause = 5 * time.Second
	// readingAsks is what the reader is asked of each picture.
	readingAsks = "Describe what is in this picture in three sentences. Name the app if it is a screenshot and you can tell."
	// "well, it makes sense to give a smaller jpeg to gaze always right?"
	// readSize is the long side, in pixels, of the JPEG the reader is given.
	readSize = 1024
	// readingMaker makes that JPEG of any kind of original kept here (libvips).
	readingMaker = "vipsthumbnail"
)

// readable is the kinds of original a JPEG for the reader is made of.
var readable = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"image/heic": true, "image/x-adobe-dng": true,
}

type readingHandler struct{ server *QNTXServer }

func (h *readingHandler) Name() string { return readingHandlerName }

func (h *readingHandler) Execute(ctx context.Context, _ *async.Job) error {
	return h.server.readWaiting(ctx)
}

// toRead is one picture this node holds that has no reading yet.
type toRead struct {
	asset  string
	fileID string
	heldAt time.Time
	// lookedFrom is when the reader began looking at it.
	lookedFrom time.Time
}

// "the image needs to stay on screen for the duration that gaze is looking and then fade back"
//
// lookingAt is the picture the reader is looking at now, and since when: what
// GET /api/files/ says of it, so it can be shown for as long as it is looked at.
type lookingAt struct {
	mu    sync.Mutex
	asset string
	since time.Time
}

func (l *lookingAt) begin(asset string, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.asset, l.since = asset, at
}

func (l *lookingAt) end() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.asset, l.since = "", time.Time{}
}

// now is the asset looked at and since when, or false.
func (l *lookingAt) now() (string, time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.asset, l.since, l.asset != ""
}

// readWaiting reads up to readPerRound of the newest pictures without a
// reading, one at a time. One that is not read is logged and left for the
// next round; the rest are still read.
func (s *QNTXServer) readWaiting(ctx context.Context) error {
	with := s.deps.cfg.Files.ReadWith
	if with == "" {
		return nil
	}
	if _, err := exec.LookPath(readingMaker); err != nil {
		return errors.Wrapf(err, "this node reads with %s and has no %s to make what it reads", with, readingMaker)
	}
	waiting, err := s.stillToRead(readPerRound)
	if err != nil || len(waiting) == 0 {
		return err
	}
	s.logger.Infow("Reading the pictures this node holds", "with", with, "waiting", len(waiting))
	read := 0
	for i, picture := range waiting {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(readPause):
			}
		}
		if err := s.readOne(ctx, with, picture); err != nil {
			s.logger.Warnw("A picture was not read; the next round reads it", "asset", picture.asset, "file_id", picture.fileID, "with", with, "error", err)
			continue
		}
		read++
	}
	s.logger.Infow("Read the pictures that waited", "with", with, "read", read, "of", len(waiting))
	return nil
}

// readOne has the provider read one picture and keeps what it read.
func (s *QNTXServer) readOne(ctx context.Context, with string, picture toRead) error {
	client, err := s.createAIClient(with, "", "files-read")
	if err != nil {
		return err
	}
	original, found, err := s.storedFile(picture.fileID)
	if err != nil {
		return err
	}
	if !found {
		return errors.Newf("this node keeps no file %s of %s", picture.fileID, picture.asset)
	}
	small, err := smallForReading(ctx, original)
	if err != nil {
		return errors.Wrapf(err, "no JPEG to read was made of %s", picture.asset)
	}
	picture.lookedFrom = time.Now()
	s.looking.begin(picture.asset, picture.lookedFrom)
	defer s.looking.end()
	ask := provider.ChatRequest{UserPrompt: readingAsks, Attachments: []provider.ContentPart{{
		Type:     "image_url",
		ImageURL: &provider.ContentPartImage{URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(small)},
	}}}
	answer, err := client.Chat(ctx, ask)
	if err != nil {
		return errors.Wrapf(err, "%s did not read %s", with, picture.asset)
	}
	if strings.TrimSpace(answer.Content) == "" {
		return errors.Newf("%s read nothing in %s", with, picture.asset)
	}
	return s.attestRead(picture, with, answer.Model, answer.Content)
}

// attestRead writes what was read in a picture, by this node, in the words of
// the provider and model that read it.
func (s *QNTXServer) attestRead(picture toRead, with, model, said string) error {
	store := s.held.TheNodesOwnRecords()
	if store == nil {
		return errors.Newf("this node holds no store to attest %s into", picture.asset)
	}
	reader := with
	if model != "" {
		reader = with + "/" + model
	}
	id, err := identity.GenerateASUIDWithRetry("AS", picture.asset, assetRead, reader, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", assetRead, picture.asset)
	}
	at := time.Now()
	attributes := map[string]any{
		"file_id": picture.fileID,
		"asked":   readingAsks,
		"read_as": fmt.Sprintf("jpeg, %d pixels on the long side", readSize),
		"said":    said,
		"read_at": at.UTC().Format(time.RFC3339),
	}
	// How long the reader looked at it, from the moment it was handed the picture.
	if !picture.lookedFrom.IsZero() {
		attributes["looked_from"] = picture.lookedFrom.UTC().Format(time.RFC3339Nano)
		attributes["looked_for_seconds"] = at.Sub(picture.lookedFrom).Seconds()
	}
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{picture.asset}, Predicates: []string{assetRead}, Contexts: []string{reader},
		Actors: []string{s.nodeDID.DID}, Timestamp: at, CreatedAt: at, Source: pluginSource,
		Attributes: attributes,
	}); err != nil {
		return errors.Wrapf(err, "the %s line of %s was not written", assetRead, picture.asset)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}

// stillToRead is up to most of the pictures this node holds that no line says
// were read, newest first.
func (s *QNTXServer) stillToRead(most int) ([]toRead, error) {
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		return nil, nil
	}
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
	wasRead := everything
	wasRead.Predicates = []string{assetRead}
	readings, err := store.GetAttestations(wasRead)
	if err != nil {
		return nil, errors.Wrap(err, "the read lines of this node did not read")
	}
	done := map[string]bool{}
	for _, line := range readings {
		for _, asset := range line.Subjects {
			done[asset] = true
		}
	}
	// The same bytes held twice are read once, and are as new as when they
	// were first held: handed over again later, a picture is no newer.
	first := map[string]toRead{}
	for _, line := range held {
		if len(line.Subjects) != 1 || done[line.Subjects[0]] {
			continue
		}
		fileType, _ := line.Attributes["file_type"].(string)
		fileID, _ := line.Attributes["file_id"].(string)
		if !readable[fileType] || fileID == "" {
			continue
		}
		asset := line.Subjects[0]
		if earlier, seen := first[asset]; seen && !line.Timestamp.Before(earlier.heldAt) {
			continue
		}
		first[asset] = toRead{asset: asset, fileID: fileID, heldAt: line.Timestamp}
	}
	waiting := make([]toRead, 0, len(first))
	for _, picture := range first {
		waiting = append(waiting, picture)
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].heldAt.After(waiting[j].heldAt) })
	if len(waiting) > most {
		waiting = waiting[:most]
	}
	return waiting, nil
}

// smallForReading is a JPEG of the original at path, readSize on its long
// side, made in a directory of its own and gone once read. The original is
// not touched.
func smallForReading(ctx context.Context, path string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "qntx-read-")
	if err != nil {
		return nil, errors.Wrap(err, "no directory to make the JPEG in")
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, "read.jpg")
	made, err := exec.CommandContext(ctx, readingMaker, path, "--size", fmt.Sprint(readSize), "-o", out+"[Q=85]").CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not make a JPEG of %s: %s", readingMaker, path, made)
	}
	small, err := os.ReadFile(out)
	if err != nil {
		return nil, errors.Wrapf(err, "the JPEG of %s did not read", path)
	}
	return small, nil
}

// readingSchedule is what the reading's schedule needs, of the jobs there are:
// a job to make, or the id of the active one to move to readEvery, or neither.
// A node that read once a night has its job moved, not a second one made.
func readingSchedule(existing []*schedule.Job, now time.Time) (make *schedule.Job, move string) {
	for _, j := range existing {
		if j.HandlerName != readingHandlerName || j.State != schedule.StateActive {
			continue
		}
		if time.Duration(j.IntervalSeconds)*time.Second == readEvery {
			return nil, ""
		}
		return nil, j.Id
	}
	return &schedule.Job{
		MadeBy:          string(async.MadeByNoCaller),
		Id:              fmt.Sprintf("SPJ_read_%d", now.Unix()),
		HandlerName:     readingHandlerName,
		IntervalSeconds: int32(readEvery.Seconds()),
		State:           schedule.StateActive,
		// The ticker compares next_run_at as text in local time.
		NextRunAt: now.Add(time.Minute).Format(time.RFC3339),
	}, ""
}

// setupReading registers the reading and keeps its schedule: every readEvery.
// A node that names no provider in files.read_with has its schedule paused.
func (s *QNTXServer) setupReading(cfg *appcfg.Config) {
	if s.held == nil {
		return
	}
	s.daemon.Registry().Register(&readingHandler{server: s})
	schedStore := s.held.ServedUniverse().Schedules()
	if cfg.Files.ReadWith == "" {
		s.pauseExistingSchedule(schedStore, readingHandlerName)
		return
	}
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("The reading is not scheduled: the schedules could not be listed", "error", err)
		return
	}
	now := time.Now()
	job, move := readingSchedule(existing, now)
	if move != "" {
		if err := schedStore.UpdateJobInterval(move, int(readEvery.Seconds())); err != nil {
			s.logger.Errorw("The reading still runs once a night: its interval was not changed", "job_id", move, "error", err)
			return
		}
		if err := schedStore.UpdateJobNextRun(move, now.Add(time.Minute)); err != nil {
			s.logger.Errorw("The reading's next run was not brought forward", "job_id", move, "error", err)
		}
		s.logger.Infow("The reading runs every few minutes, not once a night", "job_id", move, "every", readEvery)
		return
	}
	if job == nil {
		return
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("The reading is not scheduled", "error", err)
		return
	}
	s.logger.Infow("Scheduled the reading", "job_id", job.Id, "with", cfg.Files.ReadWith, "every", readEvery)
}
