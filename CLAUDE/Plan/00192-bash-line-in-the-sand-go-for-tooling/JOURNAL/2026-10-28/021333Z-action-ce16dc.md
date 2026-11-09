## 02:13 · action · —   — nvidia-status: tests red

nvidia-status: tests first, seen red. SPEC-nvidia-status.md (E1-E25, B1-B13, H1-H5) written
first; then src-go/internal/nvidiastatus's tests over a fake world (world_test.go: every
command's answer from a healthy hybrid-GPU laptop, a test breaking the part it is about) and a
stub App.Run returning 0. `go test ./internal/nvidiastatus/`: all 17 tests FAIL (help, usage
errors, the healthy report, each command once, --check silent, colour, unwritable stdout, a
hung command, and one test per section).
