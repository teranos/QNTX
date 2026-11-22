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
// 「[Opus 5.5] Once a night the node reads the newest pictures it holds that have no reading」
// 「[Opus 5.5] yet, with the provider files.read_with names, one at a time, and keeps each」
// 「[Opus 5.5] reading as a line on its picture. A picture whose reading failed has none, so」
// 「[Opus 5.5] the next night tries it again. A node that names no provider reads nothing.」

const (
	readingHandlerName = "files.read"
	// assetRead is the line that holds what was read in a picture.
	assetRead = "read"
	// readPerNight is how many pictures are read in one night.
	readPerNight = 20
	// readAtHour is the hour of the night, in the node's own time, the reading starts.
	readAtHour = 2
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
	return h.server.readTonight(ctx)
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

// readTonight reads up to readPerNight of the newest pictures without a
// reading, one at a time. One that is not read is logged and left for the
// next night; the rest are still read.
func (s *QNTXServer) readTonight(ctx context.Context) error {
	with := s.deps.cfg.Files.ReadWith
	if with == "" {
		return nil
	}
	if _, err := exec.LookPath(readingMaker); err != nil {
		return errors.Wrapf(err, "this node reads with %s and has no %s to make what it reads", with, readingMaker)
	}
	tonight, err := s.stillToRead(readPerNight)
	if err != nil {
		return err
	}
	s.logger.Infow("Reading the pictures this node holds", "with", with, "tonight", len(tonight))
	read := 0
	for i, picture := range tonight {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(readPause):
			}
		}
		if err := s.readOne(ctx, with, picture); err != nil {
			s.logger.Warnw("A picture was not read; it is read another night", "asset", picture.asset, "file_id", picture.fileID, "with", with, "error", err)
			continue
		}
		read++
	}
	s.logger.Infow("Read the pictures of tonight", "with", with, "read", read, "of", len(tonight))
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

// nextNight is the next readAtHour after now, in now's own time.
func nextNight(now time.Time) time.Time {
	at := time.Date(now.Year(), now.Month(), now.Day(), readAtHour, 0, 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

// setupNightlyReading registers the reading and keeps its schedule: once a
// day, starting at the next readAtHour. A node that names no provider in
// files.read_with has its schedule paused.
func (s *QNTXServer) setupNightlyReading(cfg *appcfg.Config) {
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
		s.logger.Errorw("The nightly reading is not scheduled: the schedules could not be listed", "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == readingHandlerName && j.State == schedule.StateActive {
			return
		}
	}
	now := time.Now()
	job := &schedule.Job{
		MadeBy:          string(async.MadeByNoCaller),
		Id:              fmt.Sprintf("SPJ_read_%d", now.Unix()),
		HandlerName:     readingHandlerName,
		IntervalSeconds: int32((24 * time.Hour).Seconds()),
		State:           schedule.StateActive,
		// The ticker compares next_run_at as text in local time.
		NextRunAt: nextNight(now).Format(time.RFC3339),
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("The nightly reading is not scheduled", "error", err)
		return
	}
	s.logger.Infow("Scheduled the nightly reading", "job_id", job.Id, "with", cfg.Files.ReadWith, "first", job.NextRunAt)
}
