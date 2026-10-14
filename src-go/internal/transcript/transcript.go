// Package transcript reads a Claude conversation's transcript: where a session's transcripts
// live, which one a restored session resumes, how big its context is, and whether it took
// input or finished a compaction after a given moment. lib/session-registry.bash keeps the
// context, input and compaction readings, uncalled, as the reference the tests compare with.
package transcript

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
)

var (
	conversationID = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	fraction       = regexp.MustCompile(`\.[0-9]+Z$`)
)

// Dir is the directory holding a session's Claude transcripts. ccy runs claude in its
// container at /workspace with /root/.claude linked to the project's .claude/ccy, so its
// transcripts are always under .claude/ccy/projects/-workspace. cc runs claude on the host,
// which names the directory for the working directory with every UTF-16 code unit that is
// not a letter or digit made a '-', as Claude does.
func Dir(prefix, dir, configHome string) (string, error) {
	switch prefix {
	case "ccy":
		return dir + "/.claude/ccy/projects/-workspace", nil
	case "cc":
		var b strings.Builder
		for _, u := range utf16.Encode([]rune(dir)) {
			if u < 0x80 && isAlnum(byte(u)) {
				b.WriteByte(byte(u))
			} else {
				b.WriteByte('-')
			}
		}
		return configHome + "/projects/" + b.String(), nil
	default:
		return "", fmt.Errorf("no transcript directory is known for launcher '%s'", prefix)
	}
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Newest is the newest *.jsonl directly in dir: the conversation `claude --continue` resumes.
// Subdirectories hold sub-agent transcripts and are not looked in. Of equal ages the first
// by name wins.
func Newest(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("there is no transcript directory %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("there is no transcript directory %s", dir)
	}
	newest := ""
	var newestTime time.Time
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		fi, statErr := os.Stat(filepath.Join(dir, name))
		if statErr != nil || !fi.Mode().IsRegular() {
			continue
		}
		if newest == "" || fi.ModTime().After(newestTime) {
			newest, newestTime = filepath.Join(dir, name), fi.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("there is no transcript in %s", dir)
	}
	return newest, nil
}

// For is the conversation a restored session resumes: <id>.jsonl for a launch with --resume
// <id>, else the newest. An id that is not a plain conversation id, or whose file is not
// there, is an error: reading another conversation would decide on the wrong one.
func For(dir, id string) (string, error) {
	if id == "" {
		return Newest(dir)
	}
	if !conversationID.MatchString(id) {
		return "", fmt.Errorf("the resumed conversation id '%s' is not a conversation id", id)
	}
	path := dir + "/" + id + ".jsonl"
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("the resumed conversation %s is not there", path)
	}
	return path, nil
}

// field is one transcript field read as T. It is tolerant as jq is: a field of any other type
// is not an error that costs the entry its other fields, it is Err and not Present, and
// null and absent are neither.
type field[T any] struct {
	Value   T
	Present bool
	Err     error
}

func (f *field[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var v T
	f.Err = json.Unmarshal(data, &v)
	if f.Err == nil {
		f.Value, f.Present = v, true
	}
	return nil
}

// flag is a field read with jq's truth: everything but null, false and absent is true.
type flag bool

func (f *flag) UnmarshalJSON(data []byte) error {
	*f = string(data) != "null" && string(data) != "false"
	return nil
}

// entry is the part of one transcript line this package reads. Every other field is ignored.
type entry struct {
	Type            field[string]          `json:"type"`
	Subtype         field[string]          `json:"subtype"`
	Timestamp       field[string]          `json:"timestamp"`
	IsSidechain     flag                   `json:"isSidechain"`
	IsMeta          flag                   `json:"isMeta"`
	Message         field[message]         `json:"message"`
	CompactMetadata field[compactMetadata] `json:"compactMetadata"`
}

type message struct {
	Model field[string] `json:"model"`
	Usage field[usage]  `json:"usage"`
}

// usage is an assistant message's token counts; absent and null count as 0 as jq's `// 0` does.
type usage struct {
	Input         field[float64] `json:"input_tokens"`
	CacheCreation field[float64] `json:"cache_creation_input_tokens"`
	CacheRead     field[float64] `json:"cache_read_input_tokens"`
}

type compactMetadata struct {
	PostTokens field[float64] `json:"postTokens"`
}

const (
	typeAssistant   = "assistant"
	typeSystem      = "system"
	typeUser        = "user"
	subtypeBoundary = "compact_boundary"
	modelSynthetic  = "<synthetic>"
)

func mainThread(e entry) bool { return !bool(e.IsSidechain) }

// count is a usage count, with absent and null counting as 0.
func count(f field[float64], name string) (float64, error) {
	if f.Err != nil {
		return 0, fmt.Errorf("%s is not a number", name)
	}
	return f.Value, nil
}

// ContextTokens is the conversation's context size in tokens: that of the last main-thread
// entry carrying one. An assistant message's input, cache-write and cache-read tokens together
// are what the next turn re-sends; a compaction boundary's postTokens is the size a compaction
// left. Sub-agent entries are another context, and a <synthetic> assistant message is an error
// placeholder with zero usage, so neither counts. A Cache answers the same from what it has
// already read; this reads the whole file.
func ContextTokens(path string) (int64, error) { return NewCache().ContextTokens(path) }

// contextOf is the context size one entry records, if it records one.
func contextOf(e entry) (float64, bool, error) {
	switch e.Type.Value {
	case typeAssistant:
		if !e.Message.Present || !e.Message.Value.Usage.Present || e.Message.Value.Model.Value == modelSynthetic {
			return 0, false, nil
		}
		u := e.Message.Value.Usage.Value
		var sum float64
		for _, c := range []struct {
			name string
			f    field[float64]
		}{{"input_tokens", u.Input}, {"cache_creation_input_tokens", u.CacheCreation}, {"cache_read_input_tokens", u.CacheRead}} {
			n, err := count(c.f, c.name)
			if err != nil {
				return 0, false, err
			}
			sum += n
		}
		return sum, true, nil
	case typeSystem:
		if !e.CompactMetadata.Present || e.Subtype.Value != subtypeBoundary {
			return 0, false, nil
		}
		post := e.CompactMetadata.Value.PostTokens
		return post.Value, post.Present, nil
	}
	return 0, false, nil
}

// TookInputSince reports whether the conversation took input at or after the Unix time since.
// Input that is submitted is written to the transcript as a main-thread user entry the moment
// it is taken (a typed /compact, its expansion, or `continue`); a line left sitting in the
// input box writes nothing. Meta entries are Claude's own notes, not input.
func TookInputSince(path string, since int64) (bool, error) {
	return NewCache().TookInputSince(path, since)
}

// CompactedSince reports whether a compaction of the conversation finished at or after the
// Unix time since (a main-thread compact_boundary entry).
func CompactedSince(path string, since int64) (bool, error) {
	return NewCache().CompactedSince(path, since)
}

// epoch is a transcript timestamp as Unix seconds: UTC, the fraction dropped.
func epoch(stamp string) (int64, error) {
	t, err := time.Parse("2006-01-02T15:04:05Z", fraction.ReplaceAllString(stamp, "Z"))
	if err != nil {
		return 0, errors.New("timestamp '" + stamp + "' is not an ISO 8601 UTC time")
	}
	return t.Unix(), nil
}
