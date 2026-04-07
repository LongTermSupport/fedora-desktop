# Plan 029: RapidRAW Cloud AI Integration

**Status**: ⬜ Not Started
**Created**: 2026-04-07
**Type**: Research → Prototype → Decision → Productionise
**Priority**: Medium

## Overview

RapidRAW (currently 1.5.3 in `play-photography.yml`) ships with three tiers of AI: built-in masking / CLIP auto-tagging / CPU inpainting (always local), self-hosted generative editing via the **RapidRAW-AI-Connector** middleware that talks HTTP to a ComfyUI server, and a future cloud subscription that does not yet exist. The user's laptop GPU (NVIDIA RTX 500 Ada Generation Laptop GPU, 4094 MiB VRAM, confirmed by `./scripts/nvidia-status.bash`) is sufficient for Tier 1 and SD 1.5 inpainting only — modern SDXL and Flux generative-fill workloads require 8–24 GB VRAM.

The user has a vast.ai account with $10 of existing credit and wants to evaluate whether remote GPU rental is a viable path to "full-capability" RapidRAW without buying hardware. This plan is structured as **research → local prototype → remote prototype → decision gate → productionise**, with each phase explicitly gated so the work can be cancelled cleanly if the cheap path turns out to be enough. The $10 of existing vast.ai credit is treated as the experimental budget; when it is burned down, the plan reaches a hard decision gate before any further spend.

The plan deliberately starts with a **free local-first phase** because the laptop's discrete NVIDIA GPU is currently *not* the default OpenGL renderer (Intel Arc iGPU is) — RapidRAW may already be silently running on the integrated GPU. Forcing the dGPU may be the entire fix and obviate the need for remote AI altogether.

## Goals

- Confirm whether RapidRAW currently uses the discrete NVIDIA GPU or the Intel Arc iGPU on the F43 laptop, and force dGPU use if not.
- Stand up the RapidRAW-AI-Connector → ComfyUI stack locally with SD 1.5 to validate the integration works end-to-end on minimal hardware before spending any money.
- Use the existing $10 vast.ai credit to prototype a remote ComfyUI workflow with SDXL and/or Flux Fill (NF4 quantised), measuring real cold-start, latency, and per-session cost on actual photos.
- Reach an evidence-based decision on whether remote GPU is worth productionising — and if so, which delivery model (vast.ai persistent / vast.ai destroy-after-session / serverless / Tailscale-to-home-GPU / hardware purchase / cancel).
- If productionisation is chosen, deliver an Ansible playbook plus per-machine `host_vars` configuration for the chosen approach, following project IaC and fail-fast rules.

## Non-Goals

- **Not buying hardware** within this plan. A dedicated GPU machine is a *possible outcome* of the decision gate but is not a deliverable of this plan.
- **Not implementing the future RapidRAW cloud subscription** — that service has not been released yet.
- **Not productionising before the prototype proves value.** Every phase has an explicit exit gate; later phases must not begin until earlier ones produce evidence.
- **Not fixing the `nvidia-vaapi-driver` drift** discovered in `play-nvidia.yml` during research — that is a separate issue (see Notes & Updates for the breadcrumb).
- **Not generalising to other hosts** in Phases 1–4. The F43 laptop is the only target until Phase 5 productionisation.

## Context & Background

### What RapidRAW's AI actually does

| Tier | Feature | Runtime | VRAM | In-scope here? |
|---|---|---|---|---|
| 1 | Subject / sky / foreground masking | Local (wgpu / Vulkan) | Any GPU | Phase 1 only (verify it works) |
| 1 | CLIP auto-tagging | Local | Any GPU | Phase 1 only |
| 1 | Lightweight inpainting | CPU | None | Phase 1 only |
| 2 | Generative inpaint via ComfyUI | Remote HTTP to ComfyUI | 4–24 GB depending on model | **Yes — entire reason this plan exists** |
| 3 | Cloud subscription | RapidRAW-managed | n/a | No — not released |

### How the connector works

`RapidRAW-AI-Connector` is a Python/FastAPI middleman (sources: [repo](https://github.com/CyberTimon/RapidRAW-AI-Connector)). RapidRAW POSTs to the connector at `localhost:5000`. The connector forwards requests to a ComfyUI server addressed via env vars `COMFY_HOST` / `COMFY_PORT`. After the first POST per image, only masks and prompts traverse the wire — full RAW data is cached server-side. Both layers have **no authentication**; any remote setup must be authenticated by network-level means (SSH tunnel or Tailscale).

RapidRAW v1.3.11 ([release notes](https://github.com/CyberTimon/RapidRAW/releases/tag/v1.3.11)) introduced a native UI for selecting workflows, mapping model names, and showing required custom nodes — meaning user-facing setup is now driven from inside RapidRAW rather than by hand-editing `workflow.json`. The user is on 1.5.3, so this UI is already present.

### Hardware reality (measured, not guessed)

From `./scripts/nvidia-status.bash` on the F43 laptop:

- dGPU: **NVIDIA RTX 500 Ada Generation Laptop GPU, 4094 MiB VRAM**
- Driver: **580.126.18**, modules loaded and signed (Secure Boot OK)
- Vulkan: working, **enumerates both Intel Arc and NVIDIA**
- OpenGL default: **Intel Arc (Mesa Intel Arc Graphics MTL)** — hybrid graphics, NVIDIA available but not default
- Hardware acceleration: `nvidia-vaapi-driver` **missing despite being listed in `play-nvidia.yml`** (drift — out of scope, see Notes & Updates)

4 GB VRAM rules out SDXL inpaint (needs ≥8 GB) and Flux Dev Fill (needs ≥12 GB), but is enough for SD 1.5 inpaint and Flux NF4 quantised variants ([rubi-du/ComfyUI-Flux-Inpainting](https://github.com/rubi-du/ComfyUI-Flux-Inpainting)).

### Cost research summary

| Provider | Pricing model | 2-hr session active cost | Idle cost | Cold start |
|---|---|---|---|---|
| vast.ai 3090 (24 GB) | Persistent | ~£0.30 | £5–12/mo storage | 1–3 min from stopped |
| vast.ai 3090 (24 GB) | Destroy-after-session + template | ~£0.30 + 15-min boot | £0 | 10–20 min |
| vast.ai 4090 (24 GB) | Persistent | ~£0.70 | £5–12/mo storage | 1–3 min |
| RunPod Serverless | Per-second | ~£0.50–1.50 | £0 | 10–30 s |
| Modal.com | Per-second | ~£0.60–1.20 | £0 | 10–20 s |
| Lambda Labs A10 | On-demand | ~£1.10 | n/a | instant |

Sources: [vast.ai live pricing](https://vast.ai/pricing), [computeprices.com — vast.ai](https://computeprices.com/providers/vast).

### vast.ai mechanics gotchas (relevant to plan design)

- vast.ai is a **peer-to-peer marketplace**. Hosts can vanish; stopped instances can be evicted.
- "Stopped" still charges storage (~$0.10–0.20/GB/month). A ComfyUI + SDXL setup is 30–80 GB → real idle cost.
- Cold start from "stopped": minutes. From a fresh template (re-download model weights): tens of minutes.
- vast.ai also offers [serverless ComfyUI templates](https://docs.vast.ai/documentation/serverless/comfy-ui), which behave more like Modal/RunPod Serverless and avoid the storage problem entirely.

### User-supplied constraints

- **$10 existing vast.ai credit** — use as the experimental budget. When burned, hit the decision gate.
- **Bursty editing pattern** assumed but not yet measured (this plan should produce real numbers).
- **Public repository** — no credentials, no real hostnames, no IPs in committed files. SSH endpoint and credentials must come from vault or `host_vars`.
- **CCY container** — all editing in this container, all deployment on host (per `CLAUDE/ContainerRules.md`).

## Tasks

### Phase 1: Free local exploration (no spend)

- [ ] ⬜ **Task 1.1**: Confirm whether RapidRAW currently uses the NVIDIA dGPU.
  - [ ] ⬜ Launch RapidRAW normally on the laptop.
  - [ ] ⬜ While it is running, run `nvidia-smi` and check the Processes table for `rapidraw`.
  - [ ] ⬜ Record finding (dGPU / iGPU / unclear) in plan Notes & Updates.
- [ ] ⬜ **Task 1.2**: If RapidRAW is on the iGPU, force NVIDIA via PRIME offload.
  - [ ] ⬜ Launch with `__NV_PRIME_RENDER_OFFLOAD=1 __VK_LAYER_NV_optimus=NVIDIA_only __GLX_VENDOR_LIBRARY_NAME=nvidia rapidraw`.
  - [ ] ⬜ Re-verify with `nvidia-smi` — confirm `rapidraw` is now in the Processes list.
  - [ ] ⬜ Subjectively assess responsiveness (mask generation latency, panning, zoom).
- [ ] ⬜ **Task 1.3**: Verify RapidRAW Settings → Processing → Compatibility Mode is **disabled** (per existing playbook note in `play-photography.yml`).
- [ ] ⬜ **Task 1.4**: Test all Tier 1 AI features on real photos.
  - [ ] ⬜ Subject masking on a portrait.
  - [ ] ⬜ Sky masking on a landscape.
  - [ ] ⬜ Foreground masking.
  - [ ] ⬜ CLIP auto-tagging on a folder of mixed photos.
  - [ ] ⬜ CPU inpainting on a small distraction.
  - [ ] ⬜ Document which Tier 1 features are sufficient and which genuinely need Tier 2.
- [ ] ⬜ **Task 1.5**: Phase 1 decision gate — does Tier 1 (with dGPU) cover ≥80% of the user's editing needs?
  - **If yes**: stop here, mark plan ✅ Complete with outcome "Tier 1 sufficient, no cloud needed". Skip Phases 2–5.
  - **If no**: proceed to Phase 2.

### Phase 2: Local ComfyUI prototype with SD 1.5 (still no spend)

Pre-condition: Phase 1 decision gate said cloud AI is worth pursuing.

- [ ] ⬜ **Task 2.1**: Install ComfyUI locally on the laptop in a throwaway directory (e.g. `~/Apps/ComfyUI`). **Manual install for prototyping only** — Ansible-isation is Phase 5.
- [ ] ⬜ **Task 2.2**: Download an SD 1.5 inpainting model that fits in 4 GB VRAM (e.g. `sd-v1-5-inpainting.ckpt`, ~4 GB). Launch ComfyUI with `--lowvram`.
- [ ] ⬜ **Task 2.3**: Install RapidRAW-AI-Connector locally.
  - [ ] ⬜ Clone the repo to `~/Apps/RapidRAW-AI-Connector`.
  - [ ] ⬜ Create a venv, `pip install -r requirements.txt`.
  - [ ] ⬜ Run `python main.py` and confirm it binds `localhost:5000`.
- [ ] ⬜ **Task 2.4**: Configure RapidRAW (via the v1.3.11 native UI) to point at `localhost:5000`.
  - [ ] ⬜ Map model name to the downloaded SD 1.5 file.
  - [ ] ⬜ Install all custom ComfyUI nodes that RapidRAW lists as required.
- [ ] ⬜ **Task 2.5**: Test inpainting on a real photo (object removal from a small region).
  - [ ] ⬜ Measure: latency per edit.
  - [ ] ⬜ Measure: GPU utilisation via `nvidia-smi dmon` during the edit.
  - [ ] ⬜ Subjectively rate output quality (1–5).
- [ ] ⬜ **Task 2.6**: Phase 2 decision gate — does the integration work end-to-end?
  - **If integration is broken**: file the bug upstream (with logs), pause plan, do not pay for remote until the local case works.
  - **If integration works but quality is acceptable**: stop here, productionise SD 1.5 locally in Phase 5 (skip Phase 3).
  - **If integration works but quality is the limiting factor**: proceed to Phase 3.

### Phase 3: Remote prototype on vast.ai using existing $10 credit

Pre-condition: Phase 2 confirmed the integration works locally; only remaining problem is VRAM/quality.

- [ ] ⬜ **Task 3.1**: Spin up an **RTX 3090 (24 GB)** instance on vast.ai using the [official ComfyUI template](https://cloud.vast.ai/template/readme/8039a8732857c7fe37dd1c42489e3217). 3090 chosen over 4090 because it is ~£0.13/hr vs ~£0.31/hr — better fit for a £8 (USD $10) experimental budget.
- [ ] ⬜ **Task 3.2**: Verify ComfyUI is reachable inside the instance via Jupyter / SSH.
- [ ] ⬜ **Task 3.3**: Download two inpaint models into the instance and document download time:
  - [ ] ⬜ An SDXL inpaint model (~6 GB).
  - [ ] ⬜ Flux Fill NF4 quantised (~6 GB).
- [ ] ⬜ **Task 3.4**: Open an SSH tunnel from the laptop forwarding `localhost:8188 → instance:8188`. **Do not bind ComfyUI to `0.0.0.0` and do not expose it publicly.**
- [ ] ⬜ **Task 3.5**: Reconfigure the local RapidRAW-AI-Connector via env vars (`COMFY_HOST=127.0.0.1 COMFY_PORT=8188`) so it now forwards to the SSH-tunnelled remote ComfyUI.
- [ ] ⬜ **Task 3.6**: Test inpainting through the full chain (RapidRAW → local connector → SSH tunnel → remote ComfyUI).
  - [ ] ⬜ First-request latency (cold cache, full RAW upload).
  - [ ] ⬜ Subsequent-request latency (warm cache, only mask + prompt).
  - [ ] ⬜ Per-edit cost (running cost / number of edits).
- [ ] ⬜ **Task 3.7**: Side-by-side quality test: SDXL inpaint vs Flux Fill NF4 on the same source photo. Document subjective verdict.
- [ ] ⬜ **Task 3.8**: Test the **destroy-and-template** strategy.
  - [ ] ⬜ Save the configured instance as a vast.ai template.
  - [ ] ⬜ Destroy the running instance.
  - [ ] ⬜ Re-provision from the template the next day.
  - [ ] ⬜ Measure boot-to-first-edit time (this is the real "session start" cost for the cheap strategy).
- [ ] ⬜ **Task 3.9**: Track total credit consumed throughout Phase 3. Halt when ≤£1 remains, regardless of completion state.

### Phase 4: Decision gate (no spend)

- [ ] ⬜ **Task 4.1**: Write a one-page evaluation as `evaluation.md` in this plan folder, covering:
  - Actual costs measured per session.
  - Subjective quality verdict (SDXL vs Flux vs SD 1.5).
  - Friction points (cold start, network, RAW upload time).
  - Honest answer: would the user pay for this regularly out of pocket?
- [ ] ⬜ **Task 4.2**: Pick **one** of these outcomes (or propose another and document it):
  - **(A) Cancel** — Tier 1 was enough, or quality wasn't worth the friction.
  - **(B) Productionise vast.ai destroy-after-session** — cheapest, accept 10–20 min setup overhead per session.
  - **(C) Productionise vast.ai persistent (stopped between sessions)** — fastest restart, accept ~£5–12/mo idle storage.
  - **(D) Switch to serverless** (RunPod Serverless / Modal / vast.ai serverless) — best for bursty short edits.
  - **(E) Defer and buy hardware** (used RTX 3060 12 GB or similar) — break-even at heavy use.
  - **(F) Tailscale-to-home-GPU** — only if the user already has or plans to acquire a beefier desktop.
- [ ] ⬜ **Task 4.3**: Update plan with chosen path; record rationale in Notes & Updates.

### Phase 5: Productionise (conditional on Phase 4 outcome)

Skip entirely for outcomes (A) or (E). For (B), (C), (D), or (F):

- [ ] ⬜ **Task 5.1**: Create a new playbook `playbooks/imports/optional/common/play-rapidraw-ai.yml` that:
  - [ ] ⬜ Installs RapidRAW-AI-Connector to a fixed path (e.g. `~/Apps/RapidRAW-AI-Connector`) via `git clone` with `creates:` for idempotency.
  - [ ] ⬜ Creates a Python venv and installs `requirements.txt`.
  - [ ] ⬜ Creates a systemd **user** service for the connector that reads `COMFY_HOST` / `COMFY_PORT` from `~/.config/rapidraw-ai-connector.env`.
  - [ ] ⬜ Templates the env file from per-machine `host_vars/{{ ansible_hostname }}.yml` variables.
  - [ ] ⬜ For outcome (B/C): installs a wrapper script in `files/usr/local/bin/rapidraw-cloud` that opens the SSH tunnel before launching RapidRAW and tears it down after.
  - [ ] ⬜ Adds a `.desktop` file override in `/usr/local/share/applications/rapidraw-cloud.desktop` for menu launch.
  - [ ] ⬜ Has the standard playbook structure: shebang, `hosts: desktop`, descriptive marker comments, fail-fast on errors.
- [ ] ⬜ **Task 5.2**: Add SSH key / endpoint configuration to vault (`environment/localhost/host_vars/localhost.yml`) using `ansible-vault encrypt_string`. **Never commit unencrypted endpoints.**
- [ ] ⬜ **Task 5.3**: Document the chosen workflow in `docs/features/rapidraw-cloud-ai.md`. Update `docs/README.md` index.
- [ ] ⬜ **Task 5.4**: Run `./scripts/qa-all.bash`. Fix anything that fails.
- [ ] ⬜ **Task 5.5**: Tell the user to deploy on the host: `ansible-playbook playbooks/imports/optional/common/play-rapidraw-ai.yml`. **Never run from CCY container** (per `CLAUDE/ContainerRules.md`).
- [ ] ⬜ **Task 5.6**: User performs a real editing session on real photos and reports back. Mark plan ✅ Complete only after this confirms the productionised path works.

## Dependencies

- **Depends on**: nothing — fully self-contained.
- **Blocks**: nothing currently.
- **Related**: none in `CLAUDE/Plan/`.

## Technical Decisions

### Decision 1: Local-first before remote
**Context**: User has a working laptop dGPU and vast.ai credit. Either could be the right answer.
**Options**:
1. Go straight to vast.ai (fast, costs credit, may discover dGPU was already enough).
2. Verify the laptop is fully utilised first (free, slower start, may make Phases 2–5 unnecessary).
**Decision**: Option 2 — Phase 1 is free and eliminates the easy answer first.
**Date**: 2026-04-07

### Decision 2: SD 1.5 local prototype before remote
**Context**: Phase 2 prototypes the integration on a model that fits in 4 GB VRAM, before paying for a 24 GB GPU.
**Options**:
1. Skip Phase 2, prototype directly on vast.ai.
2. Local SD 1.5 prototype first, then remote.
**Decision**: Option 2 — separates "is the integration broken?" from "is the model too small?". Remote debugging is much harder than local debugging, and the maintainer of RapidRAW-AI-Connector has acknowledged the integration is fragile ([discussion #292](https://github.com/CyberTimon/RapidRAW/discussions/292)).
**Date**: 2026-04-07

### Decision 3: Existing $10 credit is the experimental budget
**Context**: User has $10 sunk cost already on vast.ai. Don't spend new money until that's burned.
**Decision**: Phase 3 hard-stops when credit ≤ $1. Phase 4 (decision gate) happens with real measured numbers, not predictions. No top-ups during Phases 1–4.
**Date**: 2026-04-07

### Decision 4: SSH tunnel rather than VPN/Tailscale for Phase 3
**Context**: ComfyUI and the connector both have no authentication. Remote access must be authenticated.
**Options**:
1. SSH tunnel (zero new infra, vast.ai instances all have SSH).
2. Tailscale (requires installing Tailscale on the rented instance, needs user account binding).
3. Public ComfyUI behind a reverse proxy (insecure — rejected immediately).
**Decision**: SSH tunnel for Phase 3 prototype. Tailscale only if Phase 4 picks outcome (F) and a permanent home-GPU host exists.
**Date**: 2026-04-07

### Decision 5: RTX 3090 over 4090 for Phase 3
**Context**: Both have 24 GB. 4090 is faster but ~2.5× the price.
**Decision**: 3090. Phase 3 is about validating the workflow, not benchmarking peak quality. The £-per-hour difference matters when the budget is $10.
**Date**: 2026-04-07

## Success Criteria

- [ ] Phase 1 establishes (yes/no, with measurement) whether the laptop dGPU is being used by RapidRAW.
- [ ] Phase 2 establishes (yes/no, with screenshots/logs) whether the connector → ComfyUI integration works at all.
- [ ] Phase 3 produces real numbers: £/edit, £/session, cold-start time, perceived quality difference vs SD 1.5.
- [ ] Phase 4 produces a documented decision in `evaluation.md` with rationale.
- [ ] If productionised (Phase 5): Ansible playbook deployed, real editing session successful, `qa-all.bash` passes, plan committed alongside the playbook (per Plan Commit Rule).

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|---|---|---|---|
| Phase 1 reveals the dGPU is already in use and Tier 1 is enough → entire plan unnecessary | None (good outcome) | Medium | Phase 1 is cheap; this is a feature, not a bug |
| RapidRAW connector integration is too immature/buggy to work reliably ([maintainer admits dissatisfaction](https://github.com/CyberTimon/RapidRAW/discussions/292)) | High | Medium | Phase 2 surfaces this locally before paying for remote |
| vast.ai host evicts the persistent instance, losing the configured ComfyUI setup | Medium | Medium | Save instance config as a template after first successful setup; favour destroy-after-session strategy |
| Cold-start latency makes the workflow unusable for short edits | Medium | High | Phase 4 may pick serverless instead — that is exactly what the decision gate is for |
| RAW upload bandwidth makes first-request latency unacceptable on slow internet | Medium | Medium | Connector caches after first send; measure first-vs-warm in Phase 3 Task 3.6 |
| Credit burns down before all Phase 3 tasks are tested | Low | High | Tasks 3.1–3.7 are prioritised over 3.8; Task 3.8 (template re-provision) can be skipped if credit runs low |
| User exposes ComfyUI publicly by mistake | High | Low | All tasks specify SSH tunnel; no task tells the user to bind ComfyUI to `0.0.0.0` |
| Plan gets parked half-way through and forgotten | Low | Medium | Phase 1 is short and gives an immediate yes/no signal — clear continue-or-stop point |
| Sensitive endpoint info accidentally committed to public repo | High | Low | All endpoints in vault per project SecurityRules; pre-commit hooks already enforce this |

## Timeline

Phase ordering only (no time estimates per project rule):

1. **Phase 1** (free local) — must complete before any spend.
2. **Phase 2** (local ComfyUI) — only if Phase 1 gate says continue.
3. **Phase 3** (vast.ai prototype) — only if Phase 2 gate says continue. Bounded by $10 credit.
4. **Phase 4** (decision gate) — happens regardless of Phase 3 outcome, even partial.
5. **Phase 5** (productionise) — only if Phase 4 picks outcome B / C / D / F.

Each phase is gated; later phases can be cancelled cleanly without orphaned work.

## Notes & Updates

### 2026-04-07
- Plan created from research conversation. Full research notes and source URLs in `research.md` (sibling file).
- Diagnostic baseline established via `./scripts/nvidia-status.bash`: NVIDIA RTX 500 Ada Generation Laptop GPU, 4094 MiB VRAM, drivers 580.126.18 loaded and signed, both GPUs in Vulkan, Intel Arc is current OpenGL default (hybrid graphics).
- **Side issue discovered (out of scope for this plan)**: `play-nvidia.yml` line 38 lists `nvidia-vaapi-driver` as a required package, but `nvidia-status.bash` Section 7 reports it is **not installed** on the F43 laptop. The playbook should be erroring on this — it is not. This is a fail-fast violation that warrants its own plan or direct fix. Flagged here so it does not get lost; deliberately not folded into Plan 029's scope to keep the plan focused.
- **Discovered**: RapidRAW v1.3.11 already shipped a native ComfyUI integration UI (workflow selection, model name mapping, custom-node hints). User is on 1.5.3 so this is available — no need to hand-edit `workflow.json`. ([release notes](https://github.com/CyberTimon/RapidRAW/releases/tag/v1.3.11))
- **Discovered**: vast.ai has both [persistent ComfyUI templates](https://cloud.vast.ai/template/readme/8039a8732857c7fe37dd1c42489e3217) and [serverless ComfyUI](https://docs.vast.ai/documentation/serverless/comfy-ui). Phase 4 should consider serverless as outcome (D) even though Phase 3 uses the persistent path.
- **Discovered**: [`rubi-du/ComfyUI-Flux-Inpainting`](https://github.com/rubi-du/ComfyUI-Flux-Inpainting) wraps Flux Fill as ComfyUI nodes with reduced VRAM requirements (NF4 quantised). Worth testing in Phase 3.
