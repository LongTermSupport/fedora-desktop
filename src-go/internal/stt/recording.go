package stt

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	manifestName  = "manifest.json"
	sidecarSuffix = ".stt.md"
	// frontMatter is how every sidecar stt writes begins (render).
	frontMatter = "---\n"
	complete    = "COMPLETE"
)

var audioExtensions = []string{".ogg", ".opus", ".oga", ".m4a", ".mp3", ".wav", ".flac", ".webm", ".aac"}

// recording is one thing a sidecar is written for: a phone recording folder, or one audio file.
type recording struct {
	// path is the folder (with a manifest) or the audio file, absolute.
	path    string
	sidecar string
	// parts are the audio files in order; for a folder, filled by state.
	parts    []string
	manifest bool
	// audio is the fingerprint the sidecar records (S19), filled by state for a pending one.
	audio []audioPart
	// stale is set by state when the sidecar's fingerprint does not match the audio (S19).
	stale bool
}

// audioPart is one part of a recording as its sidecar records it (S19): from the manifest for
// a folder; for a loose file, its size, MD5 and modification time.
type audioPart struct {
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
	MD5      string `json:"md5"`
	Modified string `json:"modified,omitempty"`
}

// audioKey is the sidecar's front matter key for the fingerprint.
const audioKey = "audio"

func (r *recording) dir() string { return r.path }

type state int

const (
	statePending state = iota
	stateDone
	stateWaiting
	stateShort
	stateCorrupt
	stateStale
)

func (s state) String() string {
	return [...]string{"pending", "done", "waiting", "short", "corrupt", "stale"}[s]
}

// find is S8, S9 and S15's path checks: every recording the paths name, in name order.
func (a App) find(paths []string) ([]recording, error) {
	var recs []recording
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			a.say("cannot make " + p + " absolute: " + err.Error())
			return nil, errFailed
		}
		info, err := os.Stat(abs)
		if err != nil {
			a.say(p + " is not there: " + err.Error())
			return nil, errFailed
		}
		switch {
		case !info.IsDir() && isAudio(abs):
			recs = append(recs, audioFile(abs))
		case !info.IsDir():
			a.say(p + " is not an audio file (" + strings.Join(audioExtensions, " ") + ")")
			return nil, errFailed
		case hasManifest(abs):
			recs = append(recs, folder(abs))
		default:
			found, err := a.scan(abs)
			if err != nil {
				return nil, err
			}
			recs = append(recs, found...)
		}
	}
	return recs, nil
}

// scan is a folder of recordings, one level deep, in name order.
func (a App) scan(dir string) ([]recording, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		a.say("cannot list " + dir + ": " + err.Error())
		return nil, errFailed
	}
	var recs []recording
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir() && hasManifest(path):
			recs = append(recs, folder(path))
		case !e.IsDir() && isAudio(path):
			recs = append(recs, audioFile(path))
		}
	}
	slices.SortFunc(recs, func(x, y recording) int { return strings.Compare(x.path, y.path) })
	return recs, nil
}

func isAudio(path string) bool {
	return slices.Contains(audioExtensions, strings.ToLower(filepath.Ext(path)))
}

func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, manifestName))
	return err == nil && !info.IsDir()
}

func folder(path string) recording {
	return recording{path: path, sidecar: path + sidecarSuffix, manifest: true}
}

func audioFile(path string) recording {
	return recording{path: path, sidecar: path + sidecarSuffix, parts: []string{path}}
}

// manifest is the part of the phone app's manifest.json this reads.
type manifest struct {
	State          *string `json:"state"`
	RecordedMillis *int64  `json:"recordedMillis"`
	Segments       []struct {
		Name           string  `json:"name"`
		Bytes          *int64  `json:"bytes"`
		MD5            *string `json:"md5"`
		DurationMillis int64   `json:"durationMillis"`
	} `json:"segments"`
}

// fingerprint is the parts as the sidecar records them (S19).
func (m manifest) fingerprint() []audioPart {
	parts := make([]audioPart, 0, len(m.Segments))
	for _, s := range m.Segments {
		parts = append(parts, audioPart{Name: s.Name, Bytes: *s.Bytes, MD5: strings.ToLower(*s.MD5)})
	}
	return parts
}

// millis is the recording's length: recordedMillis, else the parts' durations added up.
func (m manifest) millis() int64 {
	if m.RecordedMillis != nil {
		return *m.RecordedMillis
	}
	var total int64
	for _, s := range m.Segments {
		total += s.DurationMillis
	}
	return total
}

// check is what state is asked to do.
type check struct {
	// verify reads each part for its MD5; without it only presence and size are checked.
	verify bool
	// minMillis is the shortest recording worth transcribing (a manifest's length only).
	minMillis int64
	// now is when the run began: a loose file changed within settleTime of it waits (S18).
	now time.Time
}

// settleTime is how long a loose audio file must go unchanged before it is sent: with no
// manifest, nothing else says that a copy into the folder has finished.
const settleTime = time.Minute

// staleReason is S19's: the sidecar's audio fingerprint does not match the recording's.
const staleReason = "the audio has changed since its transcript"

// state is S4-S7, S17-S19: done, stale, waiting, short, corrupt or pending, with the reason.
func (r *recording) state(c check) (state, string) {
	st, reason := r.readiness(c)
	if r.stale && st == statePending {
		return stateStale, staleReason
	}
	return st, reason
}

func (r *recording) readiness(c check) (state, string) {
	recorded, st, reason, found := r.sidecarState()
	if found && st != stateDone {
		return st, reason
	}
	if found {
		if recorded == nil {
			return stateDone, "" // written before sidecars carried a fingerprint
		}
		changed, why := r.audioChanged(recorded)
		if why != "" {
			return stateCorrupt, why
		}
		if !changed {
			return stateDone, ""
		}
		r.stale = true
	}
	if !r.manifest {
		return r.looseFile(c)
	}
	m, reason := readManifest(filepath.Join(r.path, manifestName))
	if reason != "" {
		return stateCorrupt, reason
	}
	r.audio = m.fingerprint()
	if *m.State != complete {
		return stateWaiting, "the manifest says " + *m.State
	}
	if length := m.millis(); length < c.minMillis {
		return stateShort, fmt.Sprintf("%.1f s", float64(length)/1000)
	}
	r.parts = make([]string, 0, len(m.Segments))
	for i, s := range m.Segments {
		part := filepath.Join(r.path, s.Name)
		if st, reason := checkPart(part, fmt.Sprintf("part %d, %s,", i+1, s.Name), *s.Bytes, *s.MD5, c.verify); st != statePending {
			return st, reason
		}
		r.parts = append(r.parts, part)
	}
	return statePending, ""
}

// sidecarState is S4: a sidecar that opens with stt's front matter is done; one that is empty
// or does not (cut short, emptied, or someone else's file) is corrupt, never quietly done.
// It returns the sidecar's audio fingerprint (S19; nil when it has none), the state, the
// reason, and whether there is a sidecar at all.
func (r *recording) sidecarState() ([]audioPart, state, string, bool) {
	f, err := os.Open(r.sidecar)
	if errors.Is(err, os.ErrNotExist) {
		return nil, statePending, "", false
	}
	if err != nil {
		return nil, stateCorrupt, "cannot read its sidecar: " + err.Error(), true
	}
	recorded, ok, err := readFrontMatter(f)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = cerr
	}
	switch {
	case err != nil:
		return nil, stateCorrupt, "cannot read its sidecar " + r.sidecar + ": " + err.Error(), true
	case !ok:
		return nil, stateCorrupt, "its sidecar " + r.sidecar + " is not a transcript (empty, or not written by stt); " +
			"move it aside to transcribe again", true
	}
	return recorded, stateDone, "", true
}

// readFrontMatter reads a sidecar's front matter: its audio line (nil when there is none), and
// false when the file does not open with front matter at all.
func readFrontMatter(f io.Reader) ([]audioPart, bool, error) {
	var recorded []audioPart
	lines := bufio.NewScanner(f)
	if !lines.Scan() || lines.Text()+"\n" != frontMatter {
		if err := lines.Err(); err != nil {
			return nil, false, fmt.Errorf("reading: %w", err)
		}
		return nil, false, nil
	}
	for lines.Scan() {
		line := lines.Text()
		if line+"\n" == frontMatter {
			break
		}
		if value, found := strings.CutPrefix(line, audioKey+": "); found {
			if err := json.Unmarshal([]byte(value), &recorded); err != nil {
				return nil, true, fmt.Errorf("its %s line does not parse: %w", audioKey, err)
			}
		}
	}
	if err := lines.Err(); err != nil {
		return nil, true, fmt.Errorf("reading: %w", err)
	}
	return recorded, true, nil
}

// audioChanged is S19: whether the recording's audio is still the audio recorded in its
// sidecar. A folder is judged by its manifest alone. A loose file whose size and time agree is
// not read; otherwise its MD5 decides. The reason is set when the audio cannot be judged.
func (r *recording) audioChanged(recorded []audioPart) (bool, string) {
	if r.manifest {
		m, reason := readManifest(filepath.Join(r.path, manifestName))
		if reason != "" {
			return true, "" // the manifest's own checks, run next, say what is wrong
		}
		return !sameParts(recorded, m.fingerprint()), ""
	}
	info, err := os.Stat(r.path)
	if err != nil {
		return false, "cannot be read: " + err.Error()
	}
	if len(recorded) != 1 {
		return true, ""
	}
	was := recorded[0]
	if info.Size() == was.Bytes && info.ModTime().UTC().Format(time.RFC3339Nano) == was.Modified {
		return false, ""
	}
	sum, err := md5File(r.path)
	if err != nil {
		return false, "cannot be read: " + err.Error()
	}
	return !strings.EqualFold(sum, was.MD5), ""
}

func sameParts(a, b []audioPart) bool {
	return slices.EqualFunc(a, b, func(x, y audioPart) bool {
		return x.Name == y.Name && x.Bytes == y.Bytes && strings.EqualFold(x.MD5, y.MD5)
	})
}

// looseFile is S18 and the fingerprint S19 records: a loose file changed less than settleTime
// before now waits; with verify, a pending one is read for its MD5.
func (r *recording) looseFile(c check) (state, string) {
	info, err := os.Stat(r.path)
	if err != nil {
		return stateCorrupt, "cannot be read: " + err.Error()
	}
	if age := c.now.Sub(info.ModTime()); age < settleTime {
		return stateWaiting, fmt.Sprintf("changed %d s ago; it may still be being written", int(age.Seconds()))
	}
	if !c.verify {
		return statePending, ""
	}
	sum, err := md5File(r.path)
	if err != nil {
		return stateCorrupt, "cannot be read: " + err.Error()
	}
	r.audio = []audioPart{{
		Name: filepath.Base(r.path), Bytes: info.Size(), MD5: sum,
		Modified: info.ModTime().UTC().Format(time.RFC3339Nano),
	}}
	return statePending, ""
}

// checkPart is one part against its manifest entry: there yet, its size, and with verify its MD5.
func checkPart(part, label string, size int64, sum string, verify bool) (state, string) {
	info, err := os.Stat(part)
	if errors.Is(err, os.ErrNotExist) {
		return stateWaiting, label + " has not synced yet"
	}
	if err != nil {
		return stateCorrupt, label + " cannot be read: " + err.Error()
	}
	if info.Size() != size {
		return stateCorrupt, label + " does not match the manifest (size)"
	}
	if !verify {
		return statePending, ""
	}
	got, err := md5File(part)
	if err != nil {
		return stateCorrupt, label + " cannot be read: " + err.Error()
	}
	if !strings.EqualFold(got, sum) {
		return stateCorrupt, label + " does not match the manifest (MD5)"
	}
	return statePending, ""
}

func readManifest(path string) (manifest, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, "cannot read " + path + ": " + err.Error()
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, path + " is not the phone app's JSON: " + err.Error()
	}
	switch {
	case m.State == nil:
		return manifest{}, path + " has no state"
	case len(m.Segments) == 0:
		return manifest{}, path + " names no parts"
	}
	for i, s := range m.Segments {
		if s.Name == "" || s.Name != filepath.Base(s.Name) || s.Name == "." || s.Name == ".." {
			return manifest{}, fmt.Sprintf("%s: part %d's name %q is not a file name", path, i+1, s.Name)
		}
		if s.Bytes == nil || s.MD5 == nil {
			return manifest{}, fmt.Sprintf("%s: part %d has no size or no MD5", path, i+1)
		}
	}
	return m, ""
}

func md5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	h := md5.New()
	_, err = io.Copy(h, f)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
