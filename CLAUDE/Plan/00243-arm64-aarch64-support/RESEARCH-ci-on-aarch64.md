# Research: moving CI to a self-hosted aarch64 runner (private repository)

Plan 00243 research note. Question: if the repository goes private and CI moves to a
self-hosted runner on an arm64 home server (Ampere Altra, aarch64, Fedora), what runs as-is,
what needs a small change, what genuinely needs x86_64, what the jobs assume of the host, what
assumes a public repository, and what CI would cost on GitHub-hosted runners once private.

Gathered 2026-11-07 against F44 at `69c72cff5` and PR #189 (`origin/arm/onto-f44`, open, all
checks green). Read-only; no runner was registered and nothing was run on aarch64.

Classes used below: **(1)** runs as-is on a native aarch64 Linux self-hosted runner;
**(2)** needs a small change; **(3)** genuinely needs x86_64.

## Headline

- **Nothing in CI genuinely needs x86_64.** No job tests the x86_64 install path (no workflow
  runs an x86 install at all), and every binary CI downloads has an upstream arm64 build.
- **Two blockers stand between `qa.yml` and an aarch64 runner, one of them not in this repo:**
  1. The Go gate (`scripts/qa-go-qa.bash`, inside `qa-all.bash`) runs go-qa from the pinned
     `github.com/LongTermSupport/go-qa-ci` module, whose tool store **refuses any platform but
     linux/amd64** for release-archive tools (golangci-lint, ast-grep, gitleaks, actionlint):
     `internal/toolstore/release.go:23` `const ReleasePlatform = "linux/amd64"`, enforced at
     `internal/toolstore/store.go:252-253` ("release archives are pinned for linux/amd64 only").
     `tools.lock` holds one sha256 per tool, all amd64. Upstream `main` (5227e49c68a0,
     2026-11-02) is unchanged. go-qa-ci is our own public repo, so the fix is ours: per-arch URL
     and per-arch checksum in `tools.lock`, then bump the pin in `src-go/qa/go.mod:6`. Every
     upstream has the arm64 asset (table below). PR #189 does not touch this.
  2. The jobs assume an **Ubuntu** runner (apt-get, AppArmor, `actions/setup-python`'s Ubuntu
     builds). On a Fedora host those steps fail; see "Host assumptions".
- PR #189 already does the arch-picked downloads in `qa.yml` (shellcheck, golangci-lint, fzf,
  gitleaks, each with a per-arch digest). After #189 the `qa.yml` install step is arch-clean.
  The Go gate is still not, and neither is the Ubuntu-ness.
- **A single runner cannot keep up with a busy week.** Last week `qa.yml` used about 17,900
  job-minutes (about 300 hours) on 4-vCPU runners, which is more than the 168 hours in a week.
  The server needs several runner instances in parallel (see "Capacity").

## Per workflow, per job: F44

### `.github/workflows/qa.yml`

| Job                                    | Class | Why                                                                                |
| -------------------------------------- | ----- | ---------------------------------------------------------------------------------- |
| `base` ("contains the default branch") | 1     | checkout + `git merge-base`. `runs-on` label change only.                          |
| `qa-all` ("qa-all.bash")               | 2     | x86 downloads, the go-qa platform refusal, apt-get, setup-python on Fedora (below) |
| `gitleaks`                             | 2     | x86 tarball at `qa.yml:243` (`gitleaks_${VER}_linux_x64.tar.gz`)                   |

`qa-all` details, F44:

| File:line                | Dependency                                           | arm64 upstream?                                                                                             |
| ------------------------ | ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `qa.yml:104-105`         | `sudo apt-get install fontconfig`                    | n/a: no apt on Fedora; fontconfig is a dnf package                                                          |
| `qa.yml:69-72`           | `actions/setup-python@v5`, 3.13                      | builds exist for arm64 but only for **Ubuntu** 22.04/24.04/26.04 (see below)                                |
| `qa.yml:74-77`           | `actions/setup-node@v4`, Node 20                     | yes, nodejs.org linux-arm64, distro-independent                                                             |
| `qa.yml:86-89`           | `actions/setup-go@v5`                                | yes, go.dev linux-arm64; `-race` is supported on linux/arm64                                                |
| `qa.yml:106-110`         | pip `ruff==0.16.8`, `semgrep==1.177.0`, `ansible`    | yes: ruff `manylinux_2_17_aarch64`, semgrep `manylinux_2_34_aarch64` wheels on PyPI; ansible is pure Python |
| `qa.yml:111-114`         | shellcheck `linux.x86_64.tar.xz`                     | yes, `shellcheck-v0.11.0.linux.aarch64.tar.xz`                                                              |
| `qa.yml:117-124`         | golangci-lint `linux-amd64.tar.gz`                   | yes, `golangci-lint-2.14.0-linux-arm64.tar.gz` (in the release checksums file)                              |
| `qa.yml:128,131`         | fzf `fzf_sha256_amd64` / `linux_amd64.tar.gz`        | yes, `fzf-0.74.5-linux_arm64.tar.gz`; `vars/fzf-pin.yml:14` already pins `fzf_sha256_arm64`                 |
| `qa.yml:147`             | `npm ci` in `extensions/`                            | yes: the lock file has no native or install-script packages                                                 |
| `qa.yml:208`             | `actions/upload-artifact@v4`                         | yes, JavaScript action                                                                                      |
| `qa-go-qa.bash` -> go-qa | go-qa-ci tool store, linux/amd64 only (see Headline) | the tools all ship arm64 (table below); go-qa itself refuses                                                |

go-qa's `[go]` tools (deadcode, govulncheck, gremlins) are built from source with
`go install`, so they are fine on arm64. Only its `[release]` tools are blocked.

The rest of `qa-all.bash` (gates in `scripts/qa-gates.tsv`) is static analysis and unit
suites with stubbed commands. The `x86_64` strings in `scripts/test-*.bash` are fixture data
(stubbed `uname -m`, kernel names), not host requirements. No gate downloads an arch-specific
artefact itself. Gates whose verdict depends on the host and need a look on a self-hosted
runner (not arch, but environment):

- `deployed-drift` compares repo scripts with the deployed copies under the runner user's
  `~/.local/bin` when they exist. On a runner whose user has fedora-desktop deployed, CI's
  verdict would depend on that host's deploy state. Use a dedicated runner user (or container)
  with nothing deployed.
- `private-names` learns install-specific names from the machine and the agent bus. On the
  server it would learn the server's own names: stricter, not wrong, but its verdict then
  differs from a hosted run's.
- `hook-handlers` is exempted by `QA_HOOK_HANDLERS_EXEMPT`, honoured "on GitHub Actions only"
  (`GITHUB_ACTIONS`), which a self-hosted runner also sets: unchanged behaviour.
- `colour-emoji-fallback` drives fontconfig against the installed fonts; Fedora's font set
  differs from the Ubuntu image's, so expect to re-check it once.
- Thirteen `scripts/test-*.bash` suites use tmux; others use `script`, `jq`, `gawk`, `setsid`,
  `setpriv`, `ssh-keygen`. The Ubuntu image supplies most of these implicitly; a Fedora runner
  host must get them from a playbook (IaC), not by hand.

### `.github/workflows/branch-hygiene.yml`

| Job              | Class | Why                                                                                                                                |
| ---------------- | ----- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `branch-hygiene` | 1     | checkout, `setup-go` (arm64 available), `scripts/qa-branch-hygiene.bash` builds a Go tool and judges git state. Label change only. |

Note: every run of this workflow failed last week (49 of 49); it is reporting the branch
policy, not a CI fault, and it is about 1 minute per run.

## Per workflow, per job: after PR #189

### `qa.yml` (post-#189)

- `qa-all`: shellcheck, golangci-lint and fzf are picked by `uname -m` with per-arch sha256
  (`case` on `x86_64|aarch64`, else exit 1). Install step becomes class 1 **for downloads**.
  Still class 2 for: apt-get fontconfig, setup-python on Fedora, and the go-qa platform refusal
  (the golangci-lint #189 installs is the `.qa-versions` copy; go-qa still fetches its own from
  `tools.lock`, amd64 only).
- `gitleaks`: picked by arch with per-arch digests (x64 `551f6fc8...`, arm64 `e4a487ee...`):
  class 1 apart from the runner label.
- `base`: unchanged, class 1.

### `.github/workflows/arm64-install.yml` (new in #189)

The real unattended install (`run.bash` headless, server profile) inside a privileged Fedora 44
container with systemd as PID 1, on GitHub's hosted arm64 runner `ubuntu-24.04-arm`
(`arm64-install.yml:32`). Triggers: pull requests into F44, pushes to F44-arm, manual.
Observed: about 25 minutes per successful run (8 runs), timeout 240.

On a native aarch64 Fedora self-hosted runner it is **class 2**, the most host-coupled job:

| Line      | Assumption                                                                                                  | On the Fedora server                                                                                                                                                                                                                           |
| --------- | ----------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `:32`     | `runs-on: ubuntu-24.04-arm` (hosted)                                                                        | becomes self-hosted labels                                                                                                                                                                                                                     |
| `:44-46`  | `sudo apt-get install podman`                                                                               | fails; podman is native on Fedora (install it by playbook)                                                                                                                                                                                     |
| `:49`     | `sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`                                                  | fails under `set -e`: no AppArmor on Fedora (SELinux instead); drop or guard                                                                                                                                                                   |
| `:62-71`  | `sudo podman build` from `registry.fedoraproject.org/fedora:44`                                             | fine: the image is multi-arch                                                                                                                                                                                                                  |
| `:81-88`  | `losetup`, `mkfs.ext4`, `podman run --privileged --systemd=always`, host `/lib/modules/$(uname -r)` mounted | works on Fedora, but needs root via `sudo` and leaves a loop device and a container named `fedora-arm64-install` behind; on a persistent host the next run collides on the name. Needs an `if: always()` teardown (container rm, `losetup -d`) |
| whole job | passwordless `sudo` for the runner user, rootful privileged podman                                          | on a hosted VM that is throwaway; on the home server it is root on the server for whoever can trigger the job                                                                                                                                  |

What it gains on the server: real Fedora kernel and module tree under the container (the hosted
lane mounts Ubuntu's), and Ampere has KVM, so a later lane could run the install in a real VM
(the VM lab) instead of a privileged container. No current job needs nested virtualisation.

## Upstream arm64 availability (checked 2026-11-07)

| Tool (pin)                 | arm64 asset                                                 |
| -------------------------- | ----------------------------------------------------------- |
| shellcheck 0.11.0          | `shellcheck-v0.11.0.linux.aarch64.tar.xz`                   |
| golangci-lint 2.14.0       | `golangci-lint-2.14.0-linux-arm64.tar.gz` (+ .rpm, .deb)    |
| fzf 0.74.5                 | `fzf-0.74.5-linux_arm64.tar.gz`                             |
| gitleaks 8.30.1            | `gitleaks_8.30.1_linux_arm64.tar.gz`                        |
| actionlint 1.7.12 (go-qa)  | `actionlint_1.7.12_linux_arm64.tar.gz`                      |
| ast-grep 0.50.0 (go-qa)    | `app-aarch64-unknown-linux-gnu.zip`                         |
| ruff 0.16.8                | `manylinux_2_17_aarch64` wheel                              |
| semgrep 1.177.0            | `manylinux_2_34_aarch64` wheel                              |
| Python 3.13 (setup-python) | `python-3.13.18-linux-24.04-arm64.tar.gz` etc.: Ubuntu-only |
| actions/runner 2.338.2     | `actions-runner-linux-arm64-2.338.2.tar.gz`                 |
| Fedora 44 base image       | multi-arch on registry.fedoraproject.org                    |

Sources: `gh release view -R <owner/repo> <tag> --json assets`; PyPI JSON API; the
`actions/python-versions` `versions-manifest.json`.

## Class (3): what genuinely needs x86_64

**None.** No x86-only binary, image or action is used, and no job exercises the x86_64 install
path. What moving everything to aarch64 loses is **x86_64 coverage of the arch-picked code
itself**: after #189, the `x86_64)` arms of the `case` blocks and their amd64 digests would no
longer be executed by any CI run, and the x86_64 desktop is still the main install target.

Option: keep one GitHub-hosted x86 job as a canary, not per push:

- `qa-all` on `ubuntu-latest` per F44 push: last week 87 F44 pushes x 54-100 min (see the
  2-vCPU note below) = about 4,700-8,700 min/week, roughly $120-225/month. Too dear.
- `qa-all` x86 **nightly** (or on release tags only): about 30 runs x up to 100 min = up to
  3,000 min/month = about $18, minus the Free plan's $12 included = **about $6/month**.
- `gitleaks` or `base` alone on hosted x86: about 1 billed minute per run; negligible.

## Host assumptions (all jobs)

- **runs-on**: every job is `ubuntu-latest` (#189 adds `ubuntu-24.04-arm`). Self-hosted needs
  e.g. `[self-hosted, linux, ARM64]`. GitHub lists Fedora 29+ as a supported runner OS and ARM64
  on Linux as supported (docs say "public preview").
- **Services / containers**: no job uses `services:` or `container:`, so Docker is not needed
  by the runner. Only arm64-install uses a container engine, and it already uses podman (rootful,
  via `sudo`).
- **sudo**: `qa-all` uses `sudo install` into `/usr/local/bin` and `sudo apt-get`; gitleaks uses
  `sudo install`; arm64-install is all `sudo podman`. On a shared home server, passwordless sudo
  for the runner user is root-equivalent. Better: tools pre-installed by a playbook (or the job
  run in a container image built by IaC) so the CI jobs need no sudo, except arm64-install.
- **apt vs dnf**: apt-get at `qa.yml:104-105` and `arm64-install.yml:44-45`.
- **setup-python on Fedora**: the action resolves versions from `actions/python-versions`, whose
  Linux builds carry an Ubuntu `platform_version` (22.04/24.04/26.04). The tool-cache matcher
  compares that with the host's os-release version, so on Fedora it finds nothing and fails
  unless 3.13 is pre-seeded in the runner's tool cache. Fedora 44 packages `python3.13`, which
  the step could use instead (the pycompat tests need `python3.13` by name on PATH,
  `qa.yml:66-68`).
- **Disk**: hosted runners have 14 GB SSD. arm64-install builds a Fedora image, installs the
  core group and the server profile and makes a 1 GB loop file per run; budget several GB per
  concurrent run, with pruning.
- **Caches**: no `actions/cache` use; the repository's Actions cache is empty. Each hosted run
  re-downloads every tool, module and wheel. A self-hosted runner with persistent Go build and
  module caches, pip and npm caches would be much faster, at the cost of hermetic runs; the
  usual compromise is ephemeral runner containers with mounted cache volumes.
- **Priority/locking**: go-qa runs under the repository's QA lock at nice 19 / ionice idle;
  the lock is per checkout, so parallel runners with separate work directories do not
  serialise each other.
- **Capacity**: last busy week, `qa-all` p50 43 min, p75 54 min, max 81 min (successful runs
  average 54 min) on 4-vCPU hosted x86. Ampere cores are slower per core than hosted EPYC, and
  `-race` is CPU-bound. At 400+ runs a week the server needs several concurrent runners.

## What assumes a public repository

Repository facts (2026-11-07): owner is an **organisation on the GitHub Free plan**; three
active rulesets on F-version branches (creation/deletion/non-fast-forward + pull request with 1
approving review; required signatures; default-branch deletion/non-fast-forward); **no required
status checks**; secret scanning and push protection **disabled**; Pages off; Actions limited to
GitHub-owned + an allowlist; fork PR approval policy `first_time_contributors`; default workflow
token permission `write` (each workflow narrows to `contents: read`); no `pull_request_target`,
`workflow_run` or `secrets.` use anywhere in `.github/`.

- **Free Actions minutes**: standard hosted runners are free on public repos only. Private repos
  draw from the plan's included usage ($12/month on Free, about 2,000 Linux x64 minutes), then
  bill. Self-hosted runner minutes are free.
- **Runner size**: public repos get 4 vCPU / 16 GB on `ubuntu-latest`; **private repos get
  2 vCPU / 8 GB**. `qa-all` already needs `QA_ALL_TIMEOUT: 120m` on 4 vCPUs (the Go gate's
  race runs, `qa.yml:195-203`); on 2 vCPUs expect up to about twice the time and a real risk of
  that timeout. Larger runners are only for Team or Enterprise Cloud, not the Free plan.
- **Hosted arm64 runners** (`ubuntu-24.04-arm`, used by #189): available to private repos too,
  billed at $0.005/min from included usage. Not public-only.
- **Rulesets / branch protection / required signatures**: rulesets are for "GitHub Team and
  GitHub Enterprise plans"; protected branches in private repos are a Team feature. On the Free
  organisation plan a private repo **loses enforcement of all three rulesets**, including
  required signed commits and the PR review rule. Keeping them needs GitHub Team (per-user
  billing; see github.com/pricing).
- **"contains the default branch"** is a job, not a required check: it uses only
  `github.event.repository.default_branch` and a full fetch with a read token, and works the
  same on a private repo. It is not wired as a required status check today, so nothing changes.
- **Secret scanning / push protection**: free on public repos (currently disabled here anyway);
  on private repos they need the paid GitHub Secret Protection add-on. The gitleaks CI job and
  the local pre-commit scanner keep working, so no current protection is lost.
- **Pages**: not used. Private Pages would need a paid plan.
- **Forks**: `allow_forking` is on. In a private repo, forks are private and only people with
  access can make them.

## Security: self-hosted runner, public vs private

- **Public repo + self-hosted runner: do not.** Any GitHub user can fork and open a pull request;
  `pull_request` workflows run the fork's code on the runner. Today's approval policy only gates
  first-time contributors, so anyone with one merged PR runs arbitrary code on the home server,
  with the runner user's sudo (root for arm64-install). A persistent runner also lets one job
  plant something for the next. GitHub's guidance is to use self-hosted runners with private
  repos and to prefer ephemeral runners.
- **Private repo**: only collaborators can push or open PRs; fork PR workflows in private repos
  are governed by the "Run workflows from fork pull requests" setting (off by default). The
  remaining risk is a compromised collaborator account or a malicious dependency or action
  (pip, npm, Go modules and release downloads run on the runner). Mitigations: ephemeral
  (`--ephemeral`) runners in a container or VM per job, a dedicated unprivileged runner user, no
  passwordless sudo outside the one install lane (which belongs in a VM), the runner host
  network-isolated from the rest of the home LAN, the default workflow token set to read, and
  third-party actions pinned by SHA.
- arm64-install is the dangerous job on a home server: rootful `--privileged` podman plus
  `losetup` means a malicious PR branch has root on the server. Running it in a throwaway KVM
  guest on the Ampere host is the safe shape.

## Billed minutes (what a private repo would pay on hosted runners)

Method: `gh run list` / runs API per workflow; `.../actions/runs/<id>/timing` for the samples
(it reports `run_duration_ms`, and `billable` 0 because the repo is public); per-job
`started_at`/`completed_at` from the jobs API, each job rounded **up** to a whole minute, as
GitHub bills. Raw data under `untracked/scratch/ci-minutes/` (not tracked).

Sample of the last 10 completed `qa.yml` runs (timing endpoint): 51-61 min wall each (all
success, all on the busy PR branch and F44). `branch-hygiene`: under 1 minute.
`arm64-install`: 24 min.

Week 2026-10-31..11-06 (busy):

| Workflow       | Runs                                                        | Job-minutes billed (rounded up per job) | Notes                                   |
| -------------- | ----------------------------------------------------------- | --------------------------------------- | --------------------------------------- |
| qa.yml         | 463 (439 with jobs: 184 success, 182 failure, 97 cancelled) | 17,945                                  | `qa-all` 17,194; gitleaks 440; base 309 |
| branch-hygiene | 49                                                          | 50                                      | all failed (policy findings)            |
| arm64-install  | 16                                                          | 243                                     | arm64 rate, not x64                     |

Week 2026-10-24..10-30 (quiet): `qa.yml` 97 runs, 3,459 job-minutes.

Run counts per week for `qa.yml` varied from about 100 to 520 over the last four weeks
(524, 105, 97, 463). Push **and** pull_request each run the full suite for a PR branch
(85 PR runs last week), so PR work is billed twice.

Monthly estimate at the current private-repo rate (x64 $0.006/min, arm64 $0.005/min, Free plan
$12 included, per GitHub docs read 2026-11-07):

| Scenario                                         | x64 minutes/month   | Cost/month after $12 included |
| ------------------------------------------------ | ------------------- | ----------------------------- |
| Quiet weeks (about 3,500/week)                   | about 15,000        | about $78                     |
| Four-week average (about 11,000/week)            | about 48,000        | about $275                    |
| Busy weeks (about 18,000/week)                   | about 78,000        | about $455                    |
| Busy, on 2-vCPU private runners (up to about 2x) | up to about 155,000 | up to about $920              |

arm64-install adds about 1,000 arm64 min/month (about $5). These are estimates from one
month of history on 4-vCPU public runners; the 2-vCPU slowdown is an estimate, not measured.

Pricing sources (read 2026-11-07):

- https://docs.github.com/en/billing/reference/actions-runner-pricing (Linux x64 2-core
  $0.006/min, Linux arm64 2-core $0.005/min, Linux 4-core $0.012/min; each job rounded up to the
  minute)
- https://docs.github.com/en/billing/concepts/product-billing/github-actions (included usage:
  Free $12, Pro $18, Team $18, Enterprise Cloud $300 per month; public-repo standard runners
  free; self-hosted runners free)
- https://docs.github.com/en/actions/reference/runners/github-hosted-runners (private
  `ubuntu-latest` 2 CPU/8 GB/14 GB; arm64 standard runners available to private repos)
- The $0.002/min self-hosted "platform charge" GitHub announced in December 2025 for March 2026
  was postponed; the current billing docs list self-hosted runners as free.

## Change list this implies (for the plan, not done here)

1. go-qa-ci: per-arch release URLs and `[checksums]` per arch; drop the linux/amd64-only
   refusal; bump the pin in `src-go/qa/go.mod`.
2. `qa.yml`: replace `ubuntu-latest` with self-hosted labels; drop `apt-get` (host or job image
   gets fontconfig, tmux and the rest by IaC); replace `setup-python` with Fedora's `python3.13`
   or a pre-seeded tool cache; keep #189's arch-picked downloads.
3. `arm64-install.yml`: drop apt-get and the AppArmor sysctl on Fedora; add an `if: always()`
   teardown (container, loop device); preferably run it in a throwaway KVM guest.
4. Runner host by IaC: a dedicated user, several ephemeral runner instances, no blanket sudo,
   caches as volumes, disk pruning, LAN isolation.
5. Keep one hosted x86 canary (nightly or release tags, about $6/month) so the x86_64 arms of
   the arch `case` blocks stay exercised.
6. If private on the Free plan: rulesets stop enforcing (signed commits, PR review). Decide
   between GitHub Team and losing them.
