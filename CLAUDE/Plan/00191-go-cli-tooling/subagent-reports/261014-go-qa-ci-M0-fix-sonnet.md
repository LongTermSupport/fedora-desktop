# go-qa-ci M0 fix

Fixed in public-safety-scan.sh: (1) grep status >1 now exits 2 naming the denylist line; (2) tree mode keeps contents, locations and paths in parallel files, path matches print "tracked path #N", and a content hit in a file whose name matches also uses the index; (3) denylist dir resolved with pwd -P; PR template item reworded to existing gates.
Tests added: invalid pattern, path-name leak, symlink bypass, git-config route, --staged, --stdin, --history (author identity, tag message, ref name). All assert no term in output.
Gates: gofmt, go vet, go test -race, golangci-lint 0 issues.
Open owner action (problem 6): set repository secret GQACI_DENYLIST; the workflow fails loudly without it until then.
