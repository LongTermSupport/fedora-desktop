# Plan 00235: the prune timer's review fixes

See JOURNAL/2026-10-28/103738Z-action-c0df82.md; commits f53435dcc, fea1cb809.

- F1: only buildah's containers are removed; another "storage" container is listed, never removed.
- F2: a stale project image needs both its creation and its last ccy launch over 30 days old.
- F3: nothing is removed while a ccy build lock is held.
- A success stamp, so a re-deploy retries a failed first run.
- A run lock, so two `--yes` runs never overlap.
