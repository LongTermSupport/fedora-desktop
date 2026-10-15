# qa-go.bash: build stage without test files

Change: `check_module` in `scripts/qa-go.bash` now runs `go build ./...` before `go vet`,
so it covers both src-go and src-go/qa. The `QA_DIR` variable was inlined to pay for the new
line, so the ratchet record (72 code lines, 2 functions, 81 statements) did not grow.

Red: a throwaway package in src-go (production file calling `onlyInTest()` defined in
`_test.go`) passes `go vet ./zz` and `go test ./zz`, but `scripts/qa-go.bash` exits 1:
`go build, without test files ... zz/prod.go:3:26: undefined: onlyInTest`.
Green: with the fixture removed, `GO-OK 44 package(s) ...`, rc 0.
The script has no test suite; the fixture was removed after use.

go-qa-ci is getting the same stage on feat/build-stage.
