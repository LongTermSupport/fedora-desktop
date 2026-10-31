## 17:17 · finding · —   — Signing plays UNRESOLVED, and the banner read before the last collection

Two defects seen on the host after a clean `run.bash --changed` batch.

1. Three UNRESOLVED references in the signing plays. `"{{ root_dir }}/vars/git-signing.yml"   # git_signing_key_path`
   in `vars_files` was classified with its quotes on: `_unquote` stripped quotes only when the
   value ended in one, so a trailing comment kept them, and a rooted path the mapper already
   follows was reported as one it cannot. Fixed in `_unquote`. `"{{ git_signing_key_path }}.pub"`
   is a host path: vars/git-signing.yml sets it to `git_signing_key`, which only host_vars sets,
   falling back to `/home/<user>/.ssh/id`. Decision: such a path is no input and not UNRESOLVED,
   because no checkout change can change it; the rule applies only when every walked file sets
   the variable that way and neither a walked file nor group_vars sets the inventory variable, so
   anything the mapper truly cannot read stays UNRESOLVED. Judged after every file is walked, so
   it does not depend on whether the vars file comes before the use.

2. The banner after the batch listed its last play as stale. The callback restarted
   host-health-collect.service with `--no-block`, so the batch ended (and meta-deploy handed a
   shell back, which printed the banner within the same second) before the collection after the
   last record had written anything, and a collection that then failed was reported to nobody.
   The refresh now restarts the unit and waits for it (timeout above the unit's TimeoutStartSec);
   a failed collection prints HEALTH-REFRESH-FAILED in the run.

Guards: the real-repo test that no UNRESOLVED value is still quoted or a followed rooted path;
the source scan that no systemctl call in helpers/play_ledger, helpers/host_health or
callback_plugins passes `--no-block`. Report: subagent-reports/261031-ledger-unresolved-and-banner-opus.md.
