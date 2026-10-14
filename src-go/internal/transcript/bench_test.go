package transcript

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// benchBytes is the size of the synthetic transcript the benchmarks poll: a long-lived session's
// transcript runs to tens of megabytes.
const benchBytes = 50 << 20

// syntheticLine is one transcript line about 4 KiB long: every fourth is a user entry, the rest
// assistant messages with usage and a large content body, as Claude writes them.
func syntheticLine(i int) string {
	body := strings.Repeat("x", 4000)
	if i%4 == 0 {
		return fmt.Sprintf(`{"type":"user","timestamp":"2026-10-08T10:%02d:00.000Z","message":{"content":"%s"}}`, i%60, body) + "\n"
	}
	return fmt.Sprintf(`{"type":"assistant","message":{"model":"m","usage":{"input_tokens":%d,"cache_creation_input_tokens":4,"cache_read_input_tokens":5},"content":"%s"}}`, i, body) + "\n"
}

// bigTranscript writes about benchBytes of transcript to a temp dir and returns its path.
func bigTranscript(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "big.jsonl")
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		b.Fatal(err)
	}
	w := bufio.NewWriter(f)
	written := 0
	for i := 0; written < benchBytes; i++ {
		line := syntheticLine(i)
		if _, err = w.WriteString(line); err != nil {
			b.Fatal(err)
		}
		written += len(line)
	}
	if err = w.Flush(); err != nil {
		b.Fatal(err)
	}
	if err = f.Close(); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkPollFullRead is what every poll cost before the cache: the whole file read and decoded.
func BenchmarkPollFullRead(b *testing.B) {
	path := bigTranscript(b)
	b.ResetTimer()
	for range b.N {
		if _, err := ContextTokens(path); err != nil {
			b.Fatal(err)
		}
	}
}
