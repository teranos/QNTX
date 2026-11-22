package server

import (
	"testing"
	"time"
)

// "newest first because,"
// "there's going to be more than 20, think more like 10K+ eventually"
// 「[Opus 5.5] What waits to be read is the newest pictures first, each once, and no picture already read.」
func TestThePicturesReadAreTheNewestNotYetRead(t *testing.T) {
	srv := createFileTestServer(t)
	older, err := srv.KeepFileForPlugin("ipluk", "older.jpeg", screenshot, "")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	// The held line is named to the second: the newer one is held a second later.
	time.Sleep(1100 * time.Millisecond)
	newer, err := srv.KeepFileForPlugin("ipluk", "newer.jpeg", append(append([]byte{}, screenshot...), " and more"...), "")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	// The same bytes again, and something that is no picture, are not read.
	if _, err := srv.KeepFileForPlugin("ipluk", "older-again.jpeg", screenshot, ""); err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	if _, err := srv.KeepFileForPlugin("ipluk", "report.pdf", pdfHeader(), ""); err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}

	waiting, err := srv.stillToRead(readPerNight)
	if err != nil {
		t.Fatalf("what waits to be read did not read: %v", err)
	}
	if len(waiting) != 2 || waiting[0].asset != newer.Asset || waiting[1].asset != older.Asset {
		t.Fatalf("waiting = %+v, want %s then %s", waiting, newer.Asset, older.Asset)
	}

	// A night holds no more than it reads.
	if one, _ := srv.stillToRead(1); len(one) != 1 || one[0].asset != newer.Asset {
		t.Errorf("one night of one = %+v, want only %s", one, newer.Asset)
	}

	// Once read, a picture waits no more.
	if err := srv.attestRead(waiting[0], "gaze", "Qwen2.5 VL 3B Instruct", "A screenshot of a chat."); err != nil {
		t.Fatalf("the reading was not written: %v", err)
	}
	if left, _ := srv.stillToRead(readPerNight); len(left) != 1 || left[0].asset != older.Asset {
		t.Errorf("after reading %s, waiting = %+v", newer.Asset, left)
	}
}

// "i see, we would want the real original for this"
// 「[Opus 5.5] A phone's HEIC and a camera's DNG are kept as the originals they are, by their」
// 「[Opus 5.5] bytes; one named so that is not is refused. Both wait to be read, as a JPEG made of them.」
func TestTheNodeKeepsTheOriginalAPhotoIs(t *testing.T) {
	srv := createFileTestServer(t)
	heic := append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), " then the picture"...)
	dng := []byte("II*\x00\x08\x00\x00\x00 then the picture")
	for filename, content := range map[string][]byte{"IMG_0001.HEIC": heic, "DSC_0001.dng": dng} {
		kept, err := srv.KeepFileForPlugin("ipluk", filename, content, "")
		if err != nil {
			t.Errorf("%s was not kept: %v", filename, err)
			continue
		}
		if kept.Asset != nameOf(content) {
			t.Errorf("%s kept as %q", filename, kept.Asset)
		}
	}
	if _, err := srv.KeepFileForPlugin("ipluk", "IMG_0002.heic", []byte("not a picture at all"), ""); err == nil {
		t.Error("a .heic that is no HEIC was kept")
	}
	if waiting, _ := srv.stillToRead(readPerNight); len(waiting) != 2 {
		t.Errorf("%d originals wait to be read, want the HEIC and the DNG", len(waiting))
	}
}

// 「[Opus 5.5] The reading starts at the next readAtHour, today when it is still ahead, else tomorrow.」
func TestTheReadingStartsAtTheNextNight(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 22, h, m, 0, 0, time.Local) }
	if got := nextNight(at(1, 30)); !got.Equal(at(readAtHour, 0)) {
		t.Errorf("from 01:30, %v", got)
	}
	if got := nextNight(at(readAtHour, 0)); !got.Equal(at(readAtHour, 0).AddDate(0, 0, 1)) {
		t.Errorf("from %02d:00, %v", readAtHour, got)
	}
	if got := nextNight(at(14, 0)); !got.Equal(at(readAtHour, 0).AddDate(0, 0, 1)) {
		t.Errorf("from 14:00, %v", got)
	}
}
