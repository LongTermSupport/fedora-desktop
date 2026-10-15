package wsi

import (
	"testing"
	"time"
)

func TestD5TheBatchRecordingLimitIsThirtySeconds(t *testing.T) {
	if maxRecording != 30*time.Second {
		t.Fatalf("got %v", maxRecording)
	}
}
