## 23:02 · decision · —   — Task 3.16: hotspot password generated in Go, shown from the NM profile

Task 3.16 added and built (the owner's decision of 2026-10-28, verbatim: "generate a simple
XKCD style password 3 words, vault it and save it. When actually running ftp-camera it should
show the password").

Decisions:

- Generation is Go: `ftp-camera --new-hotspot-password` (SPEC-ftp-camera E67), with an embedded
  word list (`src-go/internal/ftpcamera/words.txt`, 824 words of 3 to 9 letters) and
  `crypto/rand`, unit tested. No new bash and no Jinja: the play runs the binary it has just
  built.
- The vault save is one path: the save block of `tasks/ask-and-save-secret.yml` moved to
  `tasks/save-secret.yml`, which the asker and the play both include. The value is passed as a
  template, so no include's arguments hold it.
- The hotspot password is no longer asked for at all. A host report of the old prompt (its
  question never drawn in a tmux pane for hours, and Ctrl-C not ending it) made removing the
  interactive path the preferred fix. The camera FTP password prompt still uses the same
  `pause` path in `tasks/ask-and-save-secret-attempt.yml`, so it can hang in the same way. That
  is not changed here.
- The display (E68) reads the NetworkManager profile rather than the vault: the profile is what
  the camera must match (a profile made in GNOME is used with its own password), it needs no
  vault password file and makes no new plaintext copy. It is read as the user first and only
  falls back to root (sudo, which the session already uses) when NetworkManager keeps it back.

Red: the new tests did not build before the code (undefined hotspotWords, Env.Random,
hotspotInfo.conn). Green: `go test -race -shuffle=on ./internal/ftpcamera/` passes, coverage
93.0%; golangci-lint reports nothing. Host checks H6 and H7 are left to do.

Weakness to tell the owner about: three words from 824 is about 29 bits. Someone who captures
the WPA2 handshake can crack that offline. The hotspot is short-lived and only for the camera,
but four words, or a longer list, would make it much stronger.
