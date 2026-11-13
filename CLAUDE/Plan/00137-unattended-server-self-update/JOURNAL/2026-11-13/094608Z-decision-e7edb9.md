## 09:46 · decision · —   — Route G chosen and built; F-full declined

Owner decision 2026-11-13: route G; F-full declined (not deferred). G closes R3, the at-rest
copy; F-full would only remove the root-only copy, whose residual risk (password reuse after a
root compromise) does not justify reworking play-claude-yolo.yml. D5 stands.

Built G test-first: helpers/self_update/become_file.py says whether the root-only
self-update.become is one the cycle accepts (cycle.read_private plus one-line content, read as
bytes so a CRLF is caught). play-self-update.yml probes it first, asks only when it is not
usable (shared asker with the new secret_save: false, so nothing lands in localhost.yml),
fails before any change with neither file nor value, and writes the file only when the run
has a value. A host_vars value is still used (a rotated password must reach the file) and
draws a deprecation advice. Downstream outcome 2 (no file, no descriptor) is not met by
design. Server proof is Task 6.3. Report: subagent-reports/261113-route-g-become-file-opus.md.
