package server

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// "in my mind i expect to see the actual picture."
//
// 「[Opus 5.5] What this node holds, newest first, each picture once, with what was read in it:」
// 「[Opus 5.5] read off the node's own held and read lines, for whoever is to show the pictures.」

// heldPicture is one asset this node holds, as GET /api/files/ answers it.
type heldPicture struct {
	Asset    string           `json:"asset"`
	FileID   string           `json:"file_id"`
	Filename string           `json:"filename"`
	FileType string           `json:"file_type"`
	Size     int64            `json:"size"`
	HeldAt   time.Time        `json:"held_at"`
	Readings []pictureReading `json:"readings"`
}

// pictureReading is what one reader said of a picture.
type pictureReading struct {
	By     string    `json:"by"` // provider/model, e.g. gaze/Qwen2.5 VL 3B Instruct
	Said   string    `json:"said"`
	ReadAt time.Time `json:"read_at"`
}

// filesHeldMost is the most pictures one answer lists.
const filesHeldMost = 200

// handleFilesHeld answers GET /api/files/?limit=N: the pictures this node
// holds, newest first, with their readings.
func (s *QNTXServer) handleFilesHeld(w http.ResponseWriter, r *http.Request) {
	most := 50
	if asked, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && asked > 0 {
		most = min(asked, filesHeldMost)
	}
	pictures, err := s.picturesHeld(most)
	if err != nil {
		writeWrappedError(w, s.logger, err, "what this node holds did not read", http.StatusInternalServerError)
		return
	}
	respond(w, s.logger, http.StatusOK, pictures)
}

// picturesHeld is up to most of what this node holds, newest first by when it
// was first held, with every reading of each.
func (s *QNTXServer) picturesHeld(most int) ([]heldPicture, error) {
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		return []heldPicture{}, nil
	}
	store := s.held.ReadTheNodesOwnRecords()
	if store == nil {
		return []heldPicture{}, nil
	}
	everything := ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow}
	heldHere := everything
	heldHere.Predicates, heldHere.Contexts = []string{assetHeld}, []string{s.nodeDID.DID}
	held, err := store.GetAttestations(heldHere)
	if err != nil {
		return nil, err
	}
	wasRead := everything
	wasRead.Predicates = []string{assetRead}
	readings, err := store.GetAttestations(wasRead)
	if err != nil {
		return nil, err
	}
	readOf := map[string][]pictureReading{}
	for _, line := range readings {
		if len(line.Subjects) != 1 || len(line.Contexts) != 1 {
			continue
		}
		said, _ := line.Attributes["said"].(string)
		readOf[line.Subjects[0]] = append(readOf[line.Subjects[0]], pictureReading{By: line.Contexts[0], Said: said, ReadAt: line.Timestamp})
	}
	first := map[string]heldPicture{}
	for _, line := range held {
		if len(line.Subjects) != 1 {
			continue
		}
		asset := line.Subjects[0]
		if earlier, seen := first[asset]; seen && !line.Timestamp.Before(earlier.HeldAt) {
			continue
		}
		picture := heldPicture{Asset: asset, HeldAt: line.Timestamp, Readings: readOf[asset]}
		picture.FileID, _ = line.Attributes["file_id"].(string)
		picture.Filename, _ = line.Attributes["filename"].(string)
		picture.FileType, _ = line.Attributes["file_type"].(string)
		switch size := line.Attributes["size"].(type) {
		case float64:
			picture.Size = int64(size)
		case int64:
			picture.Size = size
		case int:
			picture.Size = int64(size)
		}
		if picture.Readings == nil {
			picture.Readings = []pictureReading{}
		}
		first[asset] = picture
	}
	pictures := make([]heldPicture, 0, len(first))
	for _, picture := range first {
		pictures = append(pictures, picture)
	}
	sort.SliceStable(pictures, func(i, j int) bool { return pictures[i].HeldAt.After(pictures[j].HeldAt) })
	if len(pictures) > most {
		pictures = pictures[:most]
	}
	return pictures, nil
}
