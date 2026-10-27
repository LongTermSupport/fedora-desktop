# Task 3.8, worker part: the go-qa project record

Branch `feat/00191-go-qa-record`, from F44 `0cb8e7532`. go-qa-ci pin
`v0.0.0-20261019185407-ccf4c1751f05` (`src-go/qa/go.mod`); format from its `docs/record.md`,
`docs/suppression.md` and `qa/record.toml` in the module cache.

## How the identifiers were found

A scratch copy of `src-go/qa/go-qa.toml` with `record` and `suppression` set to true (inside the
root, as `--config` requires; deleted afterwards, never staged), run as
`go tool go-qa --root .. --config qa/<scratch> run --no-fix -t suppression` from `src-go/qa`.
It printed 47 `gqaci.suppression.unrecorded` findings: 46 against `.golangci.yml`, which reduce to
26 distinct rules, and one `go::generated-header` for
`internal/pingbus/statusline/widthtable.go`.

The disabled linters (`linters.disable`) printed nothing: at this pin `docs/suppression.md` says
choosing which linters run is not a route and needs no entry. The plan's wording ("each disabled
linter") predates that, so none is recorded.

## What was written

- `src-go/qa/record.toml`: 27 `[[exception]]` entries.
  - 26 for `.golangci.yml`, one per distinct rule. Where one rule covers several exclusions
    (`gosec/G304|G703` covers seven, `gosec/G304` covers six, `tagliatelle` covers six,
    `gosec/G204` covers two, and three more cover two each), the justification names every path
    pattern it covers, because the record cannot bind an entry to one exclusion. Each
    justification is that exclusion's comment, plus the hazard the rule guards against.
  - 1 for `go::generated-header`, path `internal/pingbus/statusline/widthtable.go`
    (coordinator's decision: the file is generated from Python's unicodedata, and its header is
    the standard Go generated-file marker, not a lint suppression, so it stays).
  - All: `added_by_model = "claude-opus-5-5"`, `decided_by = "repository-owner"`,
    `decided_on = 2026-10-27`, `review_by = 2026-11-26`. No `[ratchet]`.
- `src-go/qa/go-qa.toml`: `[record]` with `deciders = ["repository-owner"]`, `max_total = 27`
  (the default of 10 would fail `gqaci.record.budget`), `history_base = "origin/F44"` (the
  default `origin/main` does not exist here, which CI would fail as an undetectable raise);
  `[record.trust.models] claude-opus = "medium"`; `[record.trust.medium] max_review_days = 30`,
  `max_count = 27` (the default of 3 would fail the budget). `record` and `suppression` stay off.

## Choices the owner's signature accepts

- **Tier.** Medium, as go-qa-ci's own record gives a Claude model. It means a 30-day review
  horizon for 27 entries; `high` (90 days, no per-tier cap of its own) is the alternative, and
  would make the `[record.trust.medium]` override unnecessary.
- **The caps** raised to 27 so the record can hold today's exceptions.

## Verification

On a scratch config with both switches on, pointing at a scratch record (the `[ratchet]` text
below, with today's date, prepended to `record.toml`):

- `go-qa record check`: `27 exceptions ... all valid`, exit 0.
- `go-qa run --no-fix -t record -t suppression`: both passed, exit 0: nothing unrecorded, no
  `gqaci.record.stale`. Switching on after the signature needs no further entry.

Without the ratchet the committed record fails only `gqaci.record.ratchet` (the owner's part).

## The text for the owner to sign

```toml
[ratchet]
high_water = 27
decided_by = "repository-owner"
decided_on = <the day of signing>
reason = "adopting the ceiling at the exceptions this repository's Go gate has today"
```

The reason drops "configuration" from the plan's example, because the 27th entry is a source
header, not configuration.
