# Debug Commands — Non-Interactive Rules

**ALWAYS provide non-interactive commands when asking users for diagnostic output.**

## Requirements

When requesting command output for troubleshooting, ensure commands:
- ✅ **Never open pagers** — Always use `--no-pager`, `| cat`, or `| head`
- ✅ **Never open editors** — Avoid commands that invoke EDITOR/PAGER
- ✅ **Are copy-paste ready** — Can be run directly without interaction
- ✅ **Have bounded output** — Use `head`, `tail`, `-n` flags to limit output

## Common Pitfalls and Fixes

```bash
# ❌ BAD - Opens interactive pager
systemctl status service-name
journalctl -u service-name
systemd-analyze cat-config file.conf

# ✅ GOOD - Non-interactive output
systemctl status service-name --no-pager -l
journalctl -u service-name -n 20 --no-pager
systemd-analyze cat-config file.conf | cat

# ❌ BAD - May open editor
git log
less /var/log/file.log
systemctl cat service-name

# ✅ GOOD - Direct output
git log --oneline -n 10
cat /var/log/file.log | tail -20
systemctl cat service-name | cat
```

## Default Flags to Remember

- `systemctl`: Add `--no-pager -l` (no pager, full output)
- `journalctl`: Add `--no-pager --since "10 minutes ago"` (no pager, last 10 minutes minimum)
  - **CRITICAL**: Never use `--since "1 minute ago"` — too short, misses context
  - Use `"10 minutes ago"` as minimum, `"20 minutes ago"` for more context
  - Can combine with `-n 100` to limit line count if needed
- Any command that might page: Pipe to `| cat` or `| head -50`

## Why This Matters

- Users may be in SSH sessions where pagers cause issues
- Copy-paste workflows break with interactive prompts
- Pager navigation is frustrating when you just want text output
- Makes it easier to paste output back to you for analysis
