# Feature Documentation

Comprehensive guides for advanced features in the fedora-desktop project.

---

## Available Guides

### [Speech-to-Text](speech-to-text.md)

GPU-accelerated voice typing for your entire desktop. Record speech with Insert key, transcribe with Whisper, optionally enhance with Claude Code AI.

**Key features:**

- GPU acceleration (NVIDIA CUDA)
- Real-time streaming mode
- AI enhancement (corporate/natural styles)
- Works in any application
- Multiple model sizes (tiny → large-v3)

**Prerequisites:** NVIDIA GPU with drivers

---

### [Claude DevTools (ccdt)](claude-devtools.md)

Visualise full Claude Code session logs in a web UI. Restores the detailed tool-output visibility
that recent Claude Code updates replaced with opaque summaries.

**Key features:**

- Auto-detects CCY project sessions vs host sessions
- On-demand Podman container (zero idle resource usage)
- Read-only mount (cannot modify session files)
- `--host` flag and explicit path support

**Prerequisites:** Podman installed

---

## Related Guides

Feature documentation that lives outside this directory:

- **[CCY (Claude Code YOLO)](../ccy.md)**: Containerised Claude Code with `--dangerously-skip-permissions` — security model, token pool, per-project images, supervisor
- **[GitHub Multi-Account](../github-multi-account.md)**: Complete multi-account workflow guide — SSH keys, OAuth tokens, per-account git configuration
- **[Nord VPN Manager](../nordvpn-installation.md)**: Interactive OpenVPN connection chooser via NetworkManager

---

## Quick Links

- [Main Documentation](../README.md)
- [Playbooks Reference](../playbooks.md) - Complete feature list
- [Containerization Guide](../containerization.md) - Docker, LXC, Distrobox
- [Installation Guide](../installation.md)

---

**Contributing**: When adding new feature documentation, please:

1. Create a new markdown file in this directory
2. Follow the structure in speech-to-text.md (Overview, Installation, Usage, Troubleshooting)
3. Add entry to this README
4. Link from main documentation where appropriate
