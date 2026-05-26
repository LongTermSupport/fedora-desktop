"""YAML config block splitting, preview, and merge logic.

Operates at the text level — no YAML parsing — so vault-encrypted values
and other Ansible-specific constructs are preserved byte-for-byte.

Used by run.bash for selective config import and merge operations.
"""

from __future__ import annotations


def split_blocks(content: str) -> list[tuple[str, str]]:
    """Split YAML content into top-level key blocks.

    Returns a list of (key_name, block_text) tuples.

    Rules:
    - A top-level key line starts at column 0, contains ':', and is not
      a comment or '---'.
    - Everything indented (or blank) below a key belongs to that key's block.
    - Comments at column 0 attach to the NEXT key, not the previous one.
    - A '---' document marker is discarded (not attached to any block).
    """
    blocks: list[tuple[str, str]] = []
    current_key: str | None = None
    current_lines: list[str] = []
    comment_buffer: list[str] = []  # column-0 comments waiting for next key

    for line in content.splitlines(True):  # keep newlines
        stripped = line.rstrip("\n")

        # YAML document marker — discard
        if stripped == "---":
            continue

        # Column-0 comment — belongs to the NEXT key
        if stripped.startswith("#"):
            if current_key is not None:
                # Flush current block before buffering the comment
                blocks.append((current_key, "".join(current_lines)))
                current_key = None
                current_lines = []
            comment_buffer.append(line)
            continue

        # Top-level key line (non-blank, starts at column 0, contains ':')
        is_key_line = (
            bool(stripped)
            and not stripped[0].isspace()
            and ":" in stripped
        )

        if is_key_line:
            if current_key is not None:
                blocks.append((current_key, "".join(current_lines)))
            current_key = stripped.split(":", 1)[0].strip()
            current_lines = comment_buffer + [line]
            comment_buffer = []
        elif current_key is not None:
            # Indented content, blank lines — part of current block
            current_lines.append(line)
        else:
            # Blank lines before first key — buffer with comments
            comment_buffer.append(line)

    # Flush final block
    if current_key is not None:
        blocks.append((current_key, "".join(current_lines)))

    return blocks


def preview_value(text: str) -> str:
    """Generate a short preview of a YAML block's value.

    - Vault-encrypted values → "[vault-encrypted]"
    - Simple key: value → the value (truncated if long)
    - Dict/list values → indented sub-lines
    - A simple value followed by trailing comments → shows the value
    """
    if "!vault" in text:
        return "[vault-encrypted]"

    lines = text.strip().splitlines()
    if not lines:
        return "(empty)"

    # Extract value from the first (key: value) line
    first_value = lines[0].split(":", 1)[1].strip() if ":" in lines[0] else ""

    # If the first line has a non-empty value, show it
    # (even if trailing comments follow)
    if first_value:
        if len(first_value) > 50:
            return first_value[:47] + "..."
        return first_value

    # Multi-line value (dict, list) — show indented sub-lines, skip comments
    sub = [
        line.rstrip()
        for line in lines[1:]
        if line.strip() and not line.lstrip().startswith("#")
    ][:5]
    if not sub:
        return "(empty)"
    return "\n" + "\n".join(f"       {line}" for line in sub)


def merge_blocks(
    local: list[tuple[str, str]],
    remote: list[tuple[str, str]],
    chooser=None,
) -> tuple[list[tuple[str, str]], dict[str, int]]:
    """Merge remote config blocks into local config blocks.

    Args:
        local: list of (key, text) from local config
        remote: list of (key, text) from remote config
        chooser: callable(action, key) -> choice string.
            action is "changed" or "new".
            For "changed": return "l" (local) or "r" (remote).
            For "new": return "a" (add) or "s" (skip).
            If None, defaults to keeping local / skipping new.

    Returns:
        (merged_blocks, stats_dict)
    """
    def _default_chooser(action, _key):
        return "l" if action == "changed" else "s"

    if chooser is None:
        chooser = _default_chooser

    local_dict = {k: v for k, v in local}
    remote_dict = {k: v for k, v in remote}
    local_keys = [k for k, _ in local]
    remote_keys = [k for k, _ in remote]

    merged: list[tuple[str, str]] = []
    stats = {"unchanged": 0, "added": 0, "kept_local": 0, "updated": 0}

    # Process local keys first (preserves local ordering)
    for key in local_keys:
        if key in remote_dict:
            if local_dict[key].strip() == remote_dict[key].strip():
                merged.append((key, local_dict[key]))
                stats["unchanged"] += 1
            else:
                choice = chooser("changed", key)
                if choice == "r":
                    merged.append((key, remote_dict[key]))
                    stats["updated"] += 1
                else:
                    merged.append((key, local_dict[key]))
                    stats["kept_local"] += 1
        else:
            merged.append((key, local_dict[key]))
            stats["kept_local"] += 1

    # Process remote-only keys (new keys to potentially add)
    for key in remote_keys:
        if key not in local_dict:
            choice = chooser("new", key)
            if choice == "a":
                merged.append((key, remote_dict[key]))
                stats["added"] += 1

    return merged, stats


def write_blocks(blocks: list[tuple[str, str]], filepath: str) -> None:
    """Write a list of (key, text) blocks to a file."""
    with open(filepath, "w") as f:
        for _, text in blocks:
            f.write(text)
            if not text.endswith("\n"):
                f.write("\n")


def parse_exclusion_input(raw: str, max_num: int) -> set[int]:
    """Parse a space/comma-separated string of numbers into a set of valid indices."""
    nums: set[int] = set()
    for part in raw.replace(",", " ").split():
        if not part.isdigit():
            print(f"  Warning: '{part}' is not a number, ignoring")
            continue
        n = int(part)
        if 1 <= n <= max_num:
            nums.add(n)
        else:
            print(f"  Warning: {n} is out of range (1-{max_num}), ignoring")
    return nums


# ─── CLI entry points (called by run.bash) ───────────────────────────────────


def _cli_selective(args: list[str]) -> None:
    """Interactive selective import: show keys, exclude some, write filtered."""
    import sys

    if len(args) != 3:
        print(f"Usage: {sys.argv[0]} selective <config_file> <output_file> <excluded_file>", file=sys.stderr)
        sys.exit(1)

    config_file, output_file, excluded_file = args

    with open(config_file) as f:
        blocks = split_blocks(f.read())

    print("\n  Keys in your saved config:\n")
    for i, (key, text) in enumerate(blocks, 1):
        print(f"    {i}) {key}: {preview_value(text)}")
    print()

    exclude_input = input("  Enter numbers to EXCLUDE (space/comma separated, Enter to keep all): ").strip()
    exclude_nums = parse_exclusion_input(exclude_input, len(blocks)) if exclude_input else set()

    excluded_keys = [blocks[n - 1][0] for n in sorted(exclude_nums)]
    kept = [(k, t) for i, (k, t) in enumerate(blocks, 1) if i not in exclude_nums]

    write_blocks(kept, output_file)

    with open(excluded_file, "w") as f:
        f.write(",".join(excluded_keys))

    if excluded_keys:
        print(f"\n  Excluded: {', '.join(excluded_keys)}")
    print(f"  Imported: {', '.join(k for k, _ in kept)}")


def _cli_merge(args: list[str]) -> None:
    """Interactive merge: diff local vs remote per key, user chooses."""
    import sys

    if len(args) != 3:
        print(f"Usage: {sys.argv[0]} merge <local_file> <remote_file> <output_file>", file=sys.stderr)
        sys.exit(1)

    local_file, remote_file, output_file = args

    with open(local_file) as f:
        local_blocks = split_blocks(f.read())
    with open(remote_file) as f:
        remote_blocks = split_blocks(f.read())

    local_dict = {k: v for k, v in local_blocks}
    remote_dict = {k: v for k, v in remote_blocks}

    print("\n  Merging remote config into local...\n")

    def interactive_chooser(action, key):
        if action == "changed":
            print(f"\n    CHANGED: {key}")
            print(f"      Local:  {preview_value(local_dict[key])}")
            print(f"      Remote: {preview_value(remote_dict[key])}")
            while True:
                choice = input("      [L]ocal / [R]emote? ").strip().lower()
                if choice in ("l", "r"):
                    return choice
                print("      Please enter L or R")
        else:  # new
            print(f"\n    NEW: {key}: {preview_value(remote_dict[key])}")
            while True:
                choice = input("      [A]dd / [S]kip? ").strip().lower()
                if choice in ("a", "s"):
                    return choice
                print("      Please enter A or S")

    # Print unchanged and local-only keys inline
    for key in [k for k, _ in local_blocks]:
        if key in remote_dict and local_dict[key].strip() == remote_dict[key].strip():
            print(f"    \u2713 {key}: unchanged")
        elif key not in remote_dict:
            print(f"    \u2022 {key}: local-only (keeping)")

    merged, stats = merge_blocks(local_blocks, remote_blocks, chooser=interactive_chooser)
    write_blocks(merged, output_file)

    print(f"\n  Merge complete: {stats['unchanged']} unchanged, {stats['added']} added, "
          f"{stats['updated']} updated from remote, {stats['kept_local']} kept local")


if __name__ == "__main__":
    import sys

    commands = {"selective": _cli_selective, "merge": _cli_merge}
    if len(sys.argv) < 2 or sys.argv[1] not in commands:
        print(f"Usage: {sys.argv[0]} {{selective|merge}} [args...]", file=sys.stderr)
        sys.exit(1)
    commands[sys.argv[1]](sys.argv[2:])
