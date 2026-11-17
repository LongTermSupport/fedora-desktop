## 01:53 · action · — — SC-C2 lost-export stream: green

Item 10 (SPEC-carry), SC-C2 lost exports by the coordinator's ruling, option (A): green.

`go test -race ./internal/stdinread/ ./internal/teamchat/{carry,admin/...,ctl}/` passes, and
`golangci-lint run` over those packages reports nothing. Every test named in the red entry passes.

- `stdinread.ReadLine`: the request's first line, bounded as Read, and the rest unread; the
  child reads its request through it (the stdin seam).
- `carry.StagedLost`: root's checks over `CarryStaging(t)/ctl/` before a byte is streamed, and
  a stream that re-digests each file. `LostExport` is `{Name, Size, SHA256}` with `Check`;
  `PlaceArgs.Stream`; `LostCap` exported.
- admin: `Request.Body` (askRun sends it after the request line, carry.place only);
  `LostStream` (Copy, End) and the Placer's third argument; ctl-child refuses bytes after a
  request of any other op.
- ctl: each lost export into a synced temporary, all verified and the stream ended before
  anything is placed, temporaries removed on any failure, linked into place last.
- SPEC-carry: SC-C2 and SX4 say which carries what, sc-D11 records the ruling; SI2 judges the
  units' active state, not their load state.
