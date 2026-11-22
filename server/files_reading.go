package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
)

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
}

// readTonight reads up to readPerNight of the newest pictures without a
// reading, one at a time. One that is not read is logged and left for the
// next night; the rest are still read.
func (s *QNTXServer) readTonight(ctx context.Context) error {
	with := s.deps.cfg.Files.ReadWith
	if with == "" {
		return nil
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
	ask := provider.ChatRequest{UserPrompt: readingAsks}
	s.attachFiles(&ask, []string{picture.fileID})
	if len(ask.Attachments) == 0 {
		return errors.Newf("the file %s of %s did not attach", picture.fileID, picture.asset)
	}
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
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{picture.asset}, Predicates: []string{assetRead}, Contexts: []string{reader},
		Actors: []string{s.nodeDID.DID}, Timestamp: at, CreatedAt: at, Source: pluginSource,
		Attributes: map[string]any{
			"file_id": picture.fileID,
			"asked":   readingAsks,
			"said":    said,
			"read_at": at.UTC().Format(time.RFC3339),
		},
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
	var waiting []toRead
	for _, line := range held {
		if len(line.Subjects) != 1 || done[line.Subjects[0]] {
			continue
		}
		fileType, _ := line.Attributes["file_type"].(string)
		fileID, _ := line.Attributes["file_id"].(string)
		if !strings.HasPrefix(fileType, "image/") || fileID == "" {
			continue
		}
		// The same bytes held twice are read once.
		done[line.Subjects[0]] = true
		waiting = append(waiting, toRead{asset: line.Subjects[0], fileID: fileID, heldAt: line.Timestamp})
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].heldAt.After(waiting[j].heldAt) })
	if len(waiting) > most {
		waiting = waiting[:most]
	}
	return waiting, nil
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
