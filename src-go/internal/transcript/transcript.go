// Package transcript reads a Claude conversation's transcript: where a session's transcripts
// live, which one a restored session resumes, how big its context is, and whether it took
// input or finished a compaction after a given moment. lib/session-registry.bash keeps the
// context, input and compaction readings, uncalled, as the reference the tests compare with.
package transcript

import (
	"bytes"
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

// entries reads the file's JSON objects, skipping every line that is not one: the last line
// can be half-written while Claude appends to it.
func entries(path string) ([]map[string]any, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("the transcript %s could not be read: %w", path, err)
	}
	var out []map[string]any
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		var v any
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		if obj, ok := v.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out, nil
}

// truthy is jq's truth: everything but null, false and absent.
func truthy(v any) bool {
	b, isBool := v.(bool)
	return v != nil && (!isBool || b)
}

func mainThread(e map[string]any) bool { return !truthy(e["isSidechain"]) }

func object(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// number is a JSON number field, with absent and null counting as 0 as jq's `// 0` does.
func number(m map[string]any, key string) (float64, error) {
	switch n := m[key].(type) {
	case nil:
		return 0, nil
	case float64:
		return n, nil
	default:
		return 0, fmt.Errorf("%s is not a number", key)
	}
}

// ContextTokens is the conversation's context size in tokens: that of the last main-thread
// entry carrying one. An assistant message's input, cache-write and cache-read tokens together
// are what the next turn re-sends; a compaction boundary's postTokens is the size a compaction
// left. Sub-agent entries are another context, and a <synthetic> assistant message is an error
// placeholder with zero usage, so neither counts.
func ContextTokens(path string) (int64, error) {
	all, err := entries(path)
	if err != nil {
		return 0, err
	}
	last, found := 0.0, false
	for _, e := range all {
		if !mainThread(e) {
			continue
		}
		size, ok, sizeErr := contextOf(e)
		if sizeErr != nil {
			return 0, fmt.Errorf("the transcript %s could not be read: %w", path, sizeErr)
		}
		if ok {
			last, found = size, true
		}
	}
	if !found || last < 0 || last != float64(int64(last)) {
		return 0, fmt.Errorf("the transcript %s records no context size", path)
	}
	return int64(last), nil
}

// contextOf is the context size one entry records, if it records one.
func contextOf(e map[string]any) (float64, bool, error) {
	switch e["type"] {
	case "assistant":
		msg, ok := object(e["message"])
		if !ok {
			return 0, false, nil
		}
		usage, ok := object(msg["usage"])
		if !ok || msg["model"] == "<synthetic>" {
			return 0, false, nil
		}
		var sum float64
		for _, key := range []string{"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
			n, err := number(usage, key)
			if err != nil {
				return 0, false, err
			}
			sum += n
		}
		return sum, true, nil
	case "system":
		meta, ok := object(e["compactMetadata"])
		if !ok || e["subtype"] != "compact_boundary" {
			return 0, false, nil
		}
		post, isNumber := meta["postTokens"].(float64)
		return post, isNumber, nil
	}
	return 0, false, nil
}

// TookInputSince reports whether the conversation took input at or after the Unix time since.
// Input that is submitted is written to the transcript as a main-thread user entry the moment
// it is taken (a typed /compact, its expansion, or `continue`); a line left sitting in the
// input box writes nothing. Meta entries are Claude's own notes, not input.
func TookInputSince(path string, since int64) (bool, error) {
	return anySince(path, since, func(e map[string]any) bool {
		return e["type"] == "user" && !truthy(e["isMeta"])
	})
}

// CompactedSince reports whether a compaction of the conversation finished at or after the
// Unix time since (a main-thread compact_boundary entry).
func CompactedSince(path string, since int64) (bool, error) {
	return anySince(path, since, func(e map[string]any) bool {
		return e["type"] == "system" && e["subtype"] == "compact_boundary"
	})
}

func anySince(path string, since int64, wanted func(map[string]any) bool) (bool, error) {
	all, err := entries(path)
	if err != nil {
		return false, err
	}
	found := false
	for _, e := range all {
		if !mainThread(e) || !wanted(e) {
			continue
		}
		stamp, isString := e["timestamp"].(string)
		if !isString {
			continue
		}
		at, parseErr := epoch(stamp)
		if parseErr != nil {
			return false, fmt.Errorf("the transcript %s could not be read: %w", path, parseErr)
		}
		if at >= since {
			found = true
		}
	}
	return found, nil
}

// epoch is a transcript timestamp as Unix seconds: UTC, the fraction dropped.
func epoch(stamp string) (int64, error) {
	t, err := time.Parse("2006-01-02T15:04:05Z", fraction.ReplaceAllString(stamp, "Z"))
	if err != nil {
		return 0, errors.New("timestamp '" + stamp + "' is not an ISO 8601 UTC time")
	}
	return t.Unix(), nil
}
