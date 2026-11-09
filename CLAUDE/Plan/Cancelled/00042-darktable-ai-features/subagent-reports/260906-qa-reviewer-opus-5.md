# QA Review — darktable AI removal + RapidRAW 1.5.8 to 1.6.3

Scope: uncommitted working tree in <repo>, branch F44.

**Verdict**: FIX-BEFORE-MERGE

No fail-fast violations, no IaC violations, no secrets, no misplaced play. The
design decision — fold the darktable version floor into `play-photography.yml`
rather than spawn a new play — is exactly right. What is wrong is completeness
of the cleanup, one stale doc reference the diff itself created, and a staging
state that would land a broken tree.

## Should fix

### 1. The cleanup misses two packages the deleted build playbook installed

`<repo>/playbooks/imports/optional/common/play-photography.yml:74-80`

The deleted `play-darktable-ai-build.yml` shipped THREE RPMs, not one. From
`git show HEAD:playbooks/imports/optional/common/play-darktable-ai-build.yml`
lines 343-348, the three artefacts named were
`darktable-{{ darktable_target_vr }}.x86_64.rpm`,
`darktable-tools-noise-{{ darktable_target_vr }}.x86_64.rpm` and
`darktable-tools-basecurve-{{ darktable_target_vr }}.x86_64.rpm`.

The plan records this as done on the host —
`<repo>/CLAUDE/Plan/Cancelled/00042-darktable-ai-features/PLAN.md`, T2.6:
"`darktable-5.4.1-100.ai.fc43` plus `-tools-noise` and `-tools-basecurve`
**confirmed on host**".

The new task upgrades only `darktable`. I checked whether RPM dependency
resolution would drag the subpackages along — it will not. Fetching the F44
spec from src.fedoraproject.org (`rpms/darktable/raw/f44/f/darktable.spec`,
lines 107-121), the `tools-noise` subpackage requires only ImageMagick and
gnuplot, and `tools-basecurve` requires only ImageMagick, dcraw and
perl-Image-ExifTool. Neither carries a
`Requires: %{name} = %{version}-%{release}` line, so nothing forces dnf to
move them.

Consequence: after this play runs, the host keeps
`darktable-tools-noise-5.4.1-100.ai.*` and
`darktable-tools-basecurve-5.4.1-100.ai.*` — locally-built RPMs from a
playbook that no longer exists, at a version below the floor the play just
asserted. The Verify task reports green because it only ever asks about
`darktable`. This is the partial-population-read-as-complete class in
`CLAUDE/AgentNotes.md` ("A partial result read as a complete one").

Fix: name all three packages in the upgrade task, or list them in the removal
loop for absence if they are not wanted at all.

### 2. Half the change is unstaged — a bare commit lands a broken tree

`git status --short`

The three playbook deletions and `files/usr/local/bin/darktable-ai` are
STAGED; every compensating edit is NOT staged:

    .claude/skills/update-versions/SKILL.md
    CLAUDE/Plan/README.md
    docs/playbooks.md
    playbooks/imports/optional/common/play-photography.yml
    playbooks/imports/optional/hardware-specific/play-nvidia.yml
    scripts/check-pinned-versions.bash

I checked what the index actually holds:

    git show :scripts/check-pinned-versions.bash | grep -c play-darktable-ai-build.yml   -> 1
    git show :playbooks/.../play-photography.yml | grep -c play-darktable-ai-build.yml   -> 2
    git show :docs/playbooks.md | grep -c play-darktable-ai                              -> 4
    ls playbooks/imports/optional/common/play-darktable-ai-build.yml
      -> No such file or directory

That is not merely untidy. `<repo>/scripts/check-pinned-versions.bash`
lines 130-133 test `[ ! -f "$file" ]` for each manifest row and, on a miss,
print "ERROR: manifest file not found" and exit 2. Committing the index as it
stands produces exactly that state.

Stage everything in one commit. The Plan Commit Rule also wants the plan move
to travel with the code, which it does — just make sure `CLAUDE/Plan/README.md`
goes with it.

### 3. update-versions skill still points at "the darktable plays"

`<repo>/.claude/skills/update-versions/SKILL.md:50-51`

The line reads: "checksum-pinning plays: `play-markless.yml`,
`play-photography.yml` (RapidRAW, ART), and the darktable plays."

The diff correctly removed `darktable` from the frontmatter description and
from the var list at line 11, then missed this one. The plays it names are
deleted in this same change. Drop "and the darktable plays".

### 4. docs/playbooks.md does not document the new updates-testing behaviour

`<repo>/docs/playbooks.md:886-890`

The entry is still just "Photography tools: Installs RAW processors, colour
management utilities, and camera tethering tools". This change makes the play
the first and only thing in the repo that enables `updates-testing`:

    grep -rn "updates-testing" --exclude-dir=.git --exclude-dir=Plan .
    -> playbooks/imports/optional/common/play-photography.yml:17,18,67,74,78
    -> nothing else

A user reading the catalogue has no way to know this play pulls a package from
Fedora's unvetted testing repo. Add a line.

### 5. Unpinned upgrade from a testing repo, in the one play built on pinning

`<repo>/playbooks/imports/optional/common/play-photography.yml:74-80`

Twelve lines above, the same file says "RapidRAW - hardcoded version for
security and reproducibility" and pins a sha256; ART likewise. The darktable
task then takes whatever build happens to be in updates-testing at run time,
unpinned. Two consequences the comment does not mention:

- Enabling the repo exposes ALL of updates-testing to the depsolver for that
  transaction, so darktable dependencies with newer testing builds are pulled
  in and permanently upgraded too. The task name says "Upgrade darktable"; its
  blast radius is larger.
- No reproducibility: two hosts running this a week apart can land different
  darktable builds.

A tighter form collapses all three tasks into one and states the intent
declaratively — give the dnf module a version-comparison spec
(`darktable >= {{ darktable_min_version }}`) with presence rather than
`latest`, keeping the enablerepo. The module accepts a comparison spec and
no-ops when an installed package already satisfies it, so the `rpm -q` probe
and the Verify task both become unnecessary. Worth confirming the idempotency
on the host before adopting.

### 6. Mock build residue is neither removed nor acknowledged

`<repo>/playbooks/imports/optional/common/play-photography.yml:91-103`

The removal loop covers `/var/tmp/darktable-ai-build` but the deleted build
playbook also left system state outside it
(`git show HEAD:.../play-darktable-ai-build.yml`):

- lines 140-146 — brought in `mock` and `rpm-build`
- lines 149-152 — `ansible.builtin.user` adding `{{ user_login }}` to the
  `mock` group, still in effect
- line 79 — `mock_result_dir: "/var/lib/mock/{{ mock_chroot }}/result"`
  holding the three built RPMs, plus the mock chroot cache

The group membership is the one that matters: a privilege grant the repo
created and no longer declares anywhere.

I am NOT suggesting the playbook rip out `/var/lib/mock` — that would be
destructive to any other mock use, and per the review rules the correct output
for "cannot be fixed safely" is a diagnosis, not an automated action. But the
journal entry at
`<repo>/CLAUDE/Plan/Cancelled/00042-darktable-ai-features/JOURNAL/00042-Journal-26-09-06.md:38-41`
claims the play "removes the retired ... install, launcher, desktop entry and
user-local GPU ONNX Runtime" without saying what it deliberately leaves.
Either revoke the `mock` group membership in the loop, or state the residue
explicitly in the journal/plan so the next reader is not misled.

## Nits

- **`failed_when` on the verify task suppresses the rc check** —
  `play-photography.yml:82-87`. `failed_when` REPLACES the default `rc != 0`
  failure, so if darktable were absent the task would evaluate `version()`
  against rpm's "package darktable is not installed" string. I ran that case
  against the installed ansible-core plugin
  (`ansible.plugins.test.core.version_compare`):

      'package darktable is not installed' -> AnsibleTemplatePluginError
          Version comparison failed: '<' not supported between 'str' and 'int'

  It still fails, so fail-fast is honoured — but with a template error rather
  than a message naming the real problem. Adding `darktable_final.rc != 0` to
  the condition fixes it.

- **darktable is brought in twice on a fresh host** —
  `play-photography.yml:50` still lists it in the bulk package list, so a clean
  machine pulls 5.4.1 from stable (about 30 MB) and then immediately replaces
  it at line 74. Folding it into finding 5 removes the duplication.

- **The extra-tag-prefix column now has zero users** — after removing the
  darktable row, every remaining MANIFEST row in
  `<repo>/scripts/check-pinned-versions.bash:82-90` has an empty 4th field.
  `SKILL.md:99` still cites "darktable's `release-`" as the example for a
  column nothing exercises, and `normalise()`'s `extra` branch (lines 98-100)
  is now dead code.

- **The final message breaks under `--skip-tags photography`** —
  `play-photography.yml:329-330` interpolates `{{ darktable_final.stdout }}`
  but the debug task is untagged while `darktable_final` is registered by a
  `photography`-tagged task. It was static text before. The same class of
  fragility already exists for `rapidraw_installed_version`, so this is
  consistent with the file, not a new class.

## Checked and clean

- **Placement in the IaC graph**: the darktable version floor lives in the play
  that owns darktable. No new play was created for what is an edit — the exact
  trap that produced `play-claude-state-hygiene.yml`. hosts/become/scope
  unchanged. `play-photography.yml` is not imported by
  `playbooks/playbook-main.yml` (grep: no match), so no ordering question
  arises.
- **Fail-fast**: no new `failed_when: false` / `ignore_errors: true`; the two
  existing ones keep their `# FAIL-FAST-OK:` annotations. The `failed_when:` at
  line 86 is a genuine probe-then-fail assertion, not a suppression. No
  skip-and-warn, no degraded fallback.
- **Idempotency**: second run — `5.6.1 is version('5.6.0','<')` is False, the
  upgrade task is skipped, verify passes, the removal loop reports ok. The
  enablerepo is transaction-scoped, so it makes no persistent repo change.
- **`version()` test usage**: verified directly against the installed
  ansible-core plugin. '5.6.1' False, '5.4.1' True, '5.6.0' False,
  '5.4.1-100.ai.fc44' True. The locally-built 5.4.1-100.ai RPM does sort below
  the floor, so the journal claim at lines 47-49 that dnf supersedes it is
  correct for the main package.
- **RapidRAW pin**: downloaded the asset and computed the hash —
  `<container-id>`,
  byte-for-byte the value at `play-photography.yml:14`. Asset name
  `03_RapidRAW_v1.6.3_ubuntu-22.04_x86_64.rpm` resolves (HTTP 200, 30873655
  bytes).
- **Fedora version claims**: confirmed via Bodhi —
  `darktable-5.6.1-2.fc44 | testing`, `darktable-5.4.1-1.fc44 | stable`. The
  plan's premise holds.
- **Leftover references**: repo-wide grep for
  `darktable-ai|darktable_ai|darktable_version|USE_AI|onnxruntime` finds hits
  only in `CLAUDE/Plan/` historical documents (correct — they are the record)
  and the two SKILL.md lines above. `docs/` has zero darktable mentions.
  `CLAUDE/AgentNotes.md` and `.claude/rules/` are clean.
  `.claude/settings.local.json` has seven stale allow-entries including a real
  home path, but it is gitignored (`.gitignore:17`) and untracked — not a
  public-repo exposure and not in the diff.
- **Naming**: `darktable_min_version` says what it is. Task names are
  action-oriented and specific; none contain a colon-space-dash pattern (the
  Ansible 2.19 nested-mapping trap). No hygiene/management/support jargon.
- **Cleanup-task precedent**: a permanent absence-enforcing migration task is
  an established repo pattern (`play-docker.yml:116,126`,
  `play-ftp-camera.yml:92`, `play-displaylink.yml:287`) — no finding.
- **Version bumps / container integrity**: nothing under
  `files/var/local/claude-yolo/**`, no Dockerfile or entrypoint change. No
  CCY_VERSION / REQUIRED_CONTAINER_VERSION bump owed. No new playbooks, so
  shebang/exec-bit rules are not triggered.
- **Plan hygiene**: status changed In Progress to Cancelled with a dated
  reason; folder moved with `git mv` (status shows R); README row moved from
  Active to Cancelled with real rationale; a new journal day-file appended, not
  edited. `CLAUDE/Plan/README.md` has no numeric statistics block to update.
- **Public-repo safety**: no real usernames, hostnames, emails, IPs or account
  IDs anywhere in the diff. `{{ user_login }}` used for the home path at line
  102, consistent with lines 252 and 300.
- **play-nvidia.yml**: comment edits only, correctly dropping the two
  darktable-AI justifications. The `nvidia_install_cuda` var and its default
  are untouched. Worth the caller knowing that cuDNN's only in-repo consumer
  was `play-darktable-ai-gpu.yml`; keeping it as a general GPU capability is a
  recorded decision, not drift.

## Mechanical gates

- **./scripts/qa-all.bash**: PASS — "QA passed: 657 files checked". Notable
  buckets: ansible (fail-fast patterns OK; no self-referential vars; no
  deprecated ansible_<fact> vars; scope+guard declarations OK; 75 playbooks
  have correct shebang+exec), ansible-syntax (77 playbooks OK: 74 under
  playbooks/imports/, 3 elsewhere), docs (63 files OK: links, anchors, playbook
  catalogue, topic index). The shellcheck (145) and semgrep partial-parse (14
  files) warnings are pre-existing and untouched by this diff.
- **hooks-daemon plan-qa --sweep**: 2 findings, 0 block, 2 advise — both
  pre-existing staleness/journal-freshness nags on unrelated plans. Plan 00042
  no longer appears, which is correct post-cancellation.
- **ansible-playbook --syntax-check**: PASS on both changed playbooks
  (play-photography.yml, play-nvidia.yml).
- **Conditional gates — not triggered, checked either way**: no `helpers/` or
  `tests/helpers/` change (qa-helper-tests.bash ran inside qa-all regardless:
  203 tests); no `extensions/**/metadata.json` change (check_extension_compat
  ran inside qa-all: 4 extensions OK); no extension JS change, so ESLint was
  not separately required (qa-all reported "js: 7 files OK").
