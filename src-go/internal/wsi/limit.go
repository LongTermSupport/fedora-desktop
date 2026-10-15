package wsi

import "time"

// The one home of batch mode's recording limit (SPEC-wsi-watermark.md D5, W16):
// scripts/qa-stt-limits.bash fails if the number appears anywhere else in this file.
const maxRecordingSeconds = 30

// maxRecording is how long a batch dictation records before it stops by itself.
const maxRecording = maxRecordingSeconds * time.Second
