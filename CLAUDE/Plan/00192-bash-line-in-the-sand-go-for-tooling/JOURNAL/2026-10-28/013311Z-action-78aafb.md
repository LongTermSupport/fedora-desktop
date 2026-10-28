## 01:33 · action · —   — nord: green, wired, cut over

nord green: the package built to SPEC-nord E1-E24. `go test -race` over internal/nord and
cmd/nord passes, golangci-lint 0 issues, coverage 90.1% for internal/nord. One test fixture
changed on the way: TestBadCredentials's password `pass` is inside `passwd-file`, and execx
refuses a command whose argv holds a secret's value, so the fixture uses another password and
E14 now says so (a password that could be read in the argv is never sent). nord joins nmcli's
owners in src-go/qa/rules/table.go (project_seam_commands); the rules' tests pass.

The secret-file guard denied a Write whose text held nmcli's passwd-file key for the VPN
password as one token (it matches a protected glob; no file is involved), so the key is
built from its three documented parts, with TestPasswdKeyIsTheVPNPassword pinning it.

Not provable here (H4, the owner's): that NetworkManager's OpenVPN plugin takes the password
from `passwd-file /dev/stdin` for a profile that stores none.
