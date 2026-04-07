# Plan 029: Research Notes

Source material gathered while drafting the plan. Kept here so PLAN.md stays focused on the *what* and *how*, while this file holds the *why I believe this*.

## RapidRAW AI feature inventory

### Tier 1 — built-in, always local

Three features ship inside RapidRAW with no external dependencies:

1. **AI masking** — subject, sky, foreground detection. Runs on **wgpu/Vulkan** (any GPU with a Vulkan driver, including iGPUs).
2. **CLIP auto-tagging** — local CLIP model assigns keywords to imported photos for searchable organisation.
3. **Lightweight inpainting** — CPU-based (no GPU), suitable only for small distractions.

Source: [RapidRAW upstream README](https://github.com/CyberTimon/RapidRAW).

### Tier 2 — generative via RapidRAW-AI-Connector + ComfyUI

The interesting tier. Architecture:

```
RapidRAW (desktop)
  ↓ HTTP POST
RapidRAW-AI-Connector (FastAPI, localhost:5000)
  ↓ HTTP (env vars: COMFY_HOST, COMFY_PORT)
ComfyUI (anywhere — local or remote)
```

Key properties:

- **Caching**: after the first POST per image, only the mask and text prompt are sent over the wire — full RAW data stays cached on the connector. This is what makes a remote ComfyUI viable on photo-sized data.
- **No authentication** at either layer. Remote access must be tunnel-protected.
- **Configurable via env vars** (`COMFY_HOST`, `COMFY_PORT`) — explicitly designed to support remote ComfyUI.
- **RapidRAW v1.3.11** added a native UI for workflow selection, model mapping, and custom-node hints. The user (on 1.5.3) has this UI already. ([release notes](https://github.com/CyberTimon/RapidRAW/releases/tag/v1.3.11))

Sources:
- [RapidRAW-AI-Connector repo](https://github.com/CyberTimon/RapidRAW-AI-Connector)
- [v1.3.11 release notes](https://github.com/CyberTimon/RapidRAW/releases/tag/v1.3.11)

### Maturity warning

[Discussion #292](https://github.com/CyberTimon/RapidRAW/discussions/292) reveals:

- The maintainer is not yet satisfied with the connector implementation.
- Common breakage mode: filename mismatches (user downloaded `XL_RealVisXL_V5.0_Lightning.safetensors` instead of the exact name in the workflow).
- Recommendation from maintainer: keep default node IDs unchanged, only update model/ControlNet/VAE *names*.

This is the reason the plan has Phase 2 (local ComfyUI) as a separate phase from Phase 3 (remote): debugging integration bugs against a free local ComfyUI is much cheaper than against a paid remote one.

### Tier 3 — official cloud subscription

Mentioned in upstream docs as "coming soon". Not yet released. Out of scope.

## Hardware reality on the F43 laptop

From `./scripts/nvidia-status.bash` output captured during the research conversation:

```
GPU: NVIDIA RTX 500 Ada Generation Laptop GPU
Driver: 580.126.18
VRAM: 4094 MiB
Vulkan API: 1.4.311
OpenGL default: Mesa Intel(R) Arc(tm) Graphics (MTL)  ← iGPU is current default
Hybrid graphics: detected
```

### What this means for AI workflows

| Workflow | VRAM needed | Fits on 4 GB? |
|---|---|---|
| RapidRAW Tier 1 (masking, CLIP, CPU inpaint) | <2 GB / CPU | ✅ Yes |
| SD 1.5 inpaint (`--lowvram`) | ~3–4 GB | ✅ Tight but works |
| SDXL inpaint | 8+ GB | ❌ No |
| Flux Dev Fill (full precision) | 12–24 GB | ❌ No |
| Flux Fill NF4 (quantised) | ~6–8 GB | ❌ Likely no, unless extreme tiling |
| Wan2.2 5B video models | 10+ GB | ❌ No |

So the laptop can do Tier 1 today and SD 1.5 inpaint tomorrow. Anything modern and generative needs >4 GB → either remote GPU or hardware purchase.

### The hybrid-graphics trap

The OpenGL default renderer is Intel Arc. Vulkan enumerates *both* GPUs, but RapidRAW (which uses wgpu) may pick whichever Vulkan adapter the driver lists first — and that's typically the iGPU. The user has not yet confirmed which GPU RapidRAW is actually using. **This is the most important Phase 1 measurement** — it could turn the entire plan into a 5-minute fix.

The fix, if needed, is well-known PRIME offload:

```
__NV_PRIME_RENDER_OFFLOAD=1 __VK_LAYER_NV_optimus=NVIDIA_only __GLX_VENDOR_LIBRARY_NAME=nvidia rapidraw
```

For permanent productionisation, this would go into a wrapper script + `.desktop` file override.

## Cost research — vast.ai and alternatives

### Hourly rates (verified live)

| GPU | VRAM | vast.ai on-demand | vast.ai interruptible | RunPod community | Lambda Labs |
|---|---|---|---|---|---|
| RTX 3090 | 24 GB | ~$0.13–0.20 | ~$0.08–0.12 | ~$0.34 | n/a |
| RTX 4090 | 24 GB | ~$0.31–0.45 | ~$0.18–0.25 | ~$0.69 | n/a |
| A5000 | 24 GB | ~$0.20–0.30 | ~$0.12–0.18 | n/a | n/a |
| A10 | 24 GB | n/a | n/a | n/a | ~$0.75 |

Sources: [vast.ai live pricing](https://vast.ai/pricing), [RTX 3090 pricing page](https://vast.ai/pricing/gpu/RTX-3090), [computeprices.com](https://computeprices.com/providers/vast).

### vast.ai pricing model gotchas

Critical things the user's "start/stop as needed" mental model misses:

1. **Stopped instances pay for storage.** ~$0.10–0.20/GB/month. ComfyUI + SDXL + Flux setup is realistically 30–80 GB → **$3–16/month idle cost** while you're not using it.
2. **Hosts can evict stopped instances.** vast.ai is peer-to-peer. The host can reclaim disk space; you may come back to nothing.
3. **Cold start is not instant.**
   - From "stopped": 1–3 minutes for VM, plus 30–60s for ComfyUI to warm models into VRAM.
   - From "destroyed" (re-provision from template): 10–30 minutes including re-downloading model weights.
4. **Per-second billing while running** is genuinely cheap. A 2-hour edit session on a 3090 is ~£0.30.

This makes the **destroy-after-session-with-template** strategy attractive: zero idle cost, accept ~15 min boot time per session.

### Serverless alternatives

For bursty single-edit workloads, serverless is genuinely a better fit:

- **vast.ai serverless** ([docs](https://docs.vast.ai/documentation/serverless/comfy-ui)) — has an official ComfyUI template, pay per request. Same provider, different billing model.
- **RunPod Serverless** — per-second-of-execution billing, ~10–30s cold start, no idle cost.
- **Modal.com** — Python-native serverless GPU, ~10–20s cold start, no idle cost.

These are worth considering for outcome (D) in the Phase 4 decision gate, but the user has $10 of vast.ai credit specifically — so Phase 3 prototypes vast.ai persistent first.

### Hardware break-even

A used RTX 3060 12 GB desktop runs ~£250 used. At £15/month vast.ai persistent, break-even ≈ 17 months. At £5/month destroy-after-session, ≈ 50 months. So hardware only beats cloud if usage is *heavy* and *sustained* — the plan's Phase 4 should not pick (E) without real evidence that this is the case.

## ComfyUI workflow research

### Existing inpainting workflows / models worth testing

| Source | Model / Approach | VRAM | Notes |
|---|---|---|---|
| [comfyui-inpaint-nodes (Acly)](https://github.com/Acly/comfyui-inpaint-nodes) | Better inpaint nodes for SDXL: Fooocus inpaint model, LaMa, MAT | 8+ GB | Standard SDXL inpaint enhancement |
| [ComfyUI-Flux-Inpainting (rubi-du)](https://github.com/rubi-du/ComfyUI-Flux-Inpainting) | Flux Fill wrapper with low-VRAM optimisations | 6–12 GB | Targets exactly the "modest GPU + Flux" niche |
| [Flux Fill official tutorial](https://comfyui-wiki.com/en/tutorial/advanced/image/flux/flux-1-dev-fill) | Flux Fill Dev | 12–24 GB | Best quality, biggest VRAM ask |
| [LanPaint](https://github.com/scraed/LanPaint) | Training-free inpaint for any SD model | varies | Worth a test if SDXL inpaint quality is poor |
| RapidRAW built-in default workflow | Unknown specific model | varies | First thing to try in Phase 2 — let RapidRAW tell you what nodes/models it wants |

### vast.ai ComfyUI templates

- [Official vast.ai ComfyUI template](https://cloud.vast.ai/template/readme/8039a8732857c7fe37dd1c42489e3217) — preinstalled CUDA, Jupyter, ComfyUI, SSH. Models and custom nodes still need to be added (provisioning script or post-boot).
- vast.ai also has a `getting started with ComfyUI` [article](https://vast.ai/article/getting-started-with-comfy-UI) and a [serverless ComfyUI guide](https://vast.ai/article/comfyui-serverless).

### Workflow JSON format for RapidRAW connector

From the RapidRAW-AI-Connector repo, default `workflow.json` defines:

- Node IDs (must be preserved if reusing the default workflow)
- Model filenames (the user's main customisation point)
- Sampler / VAE / ControlNet references

The v1.3.11 RapidRAW UI lets the user remap these without editing JSON by hand.

## Security considerations

Both ComfyUI and RapidRAW-AI-Connector have **no authentication**. This is acceptable on `localhost` but a serious risk on a public network. Plan rules:

- Phase 3 uses an SSH tunnel — `ssh -L 8188:localhost:8188 user@instance`. ComfyUI binds `127.0.0.1:8188` only.
- Phase 5 (productionise) stores SSH endpoint info in vault per `CLAUDE/SecurityRules.md`.
- No task tells the user to expose either service publicly.

## Out-of-scope side issue: nvidia-vaapi-driver drift

Discovered during research — `play-nvidia.yml` line 38:

```yaml
- name: Install NVIDIA driver packages
  ansible.builtin.dnf:
    name:
      ...
      - nvidia-vaapi-driver # For hardware video acceleration
```

But `nvidia-status.bash` Section 7 reports:

```
❌ nvidia-vaapi-driver: Not installed
ℹ  Package missing from installation - update playbook to install it
```

This is a fail-fast violation: the playbook claims it installs the package but doesn't, and `dnf` is not erroring. Possible causes:
- F43 package rename
- Repo conflict
- `dnf` skipping silently because of metadata staleness

This is **out of scope for Plan 029**, but flagged in the plan's Notes & Updates so it doesn't get lost. Worth its own short fix plan or a direct edit to `play-nvidia.yml` after F43 dnf metadata investigation.
