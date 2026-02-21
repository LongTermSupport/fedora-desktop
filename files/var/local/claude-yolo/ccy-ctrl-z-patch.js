#!/usr/bin/env node
/**
 * CCY ctrl+z suspend patch for Claude Code (Ink framework).
 *
 * Ink intercepts ctrl+z BEFORE the keybinding system and calls
 * process.kill(pid, 'SIGSTOP') — an unblockable signal. In a CCY container,
 * this makes Claude unrecoverable (no shell to run `fg`).
 *
 * This patch extends the platform condition that gates ctrl+z handling
 * to also check the CCY_DISABLE_SUSPEND environment variable.
 *
 * Before (example, variable name may differ between versions):
 *   fG5 = process.platform !== "win32"
 * After:
 *   fG5 = process.platform !== "win32" && !process.env.CCY_DISABLE_SUSPEND
 *
 * The patch uses two strategies in order:
 *   1. Known patterns: exact strings confirmed in previous Claude Code versions.
 *   2. Dynamic discovery: regex search near the handleSuspend() call site.
 *
 * The patch is BEST-EFFORT: if both strategies fail, we warn but do NOT fail
 * the build (soft-fail). Ctrl+z may freeze the container in that case.
 * See CLAUDE.md "KNOWN FRAGILE PATCH" section for manual fix instructions.
 */

'use strict';

const fs = require('fs');

const CLI_PATH = '/usr/local/lib/node_modules/@anthropic-ai/claude-code/cli.js';
const SUSPEND_GUARD = '&&!process.env.CCY_DISABLE_SUSPEND';

// Read cli.js
let src;
try {
    src = fs.readFileSync(CLI_PATH, 'utf8');
} catch (err) {
    process.stderr.write('CCY PATCH ERROR: Cannot read cli.js: ' + err.message + '\n');
    process.exit(1);
}

// Guard: don't double-patch if already applied
if (src.includes(SUSPEND_GUARD)) {
    process.stdout.write('CCY PATCH: ctrl+z suspend patch already applied - skipping\n');
    process.exit(0);
}

// Strategy 1: Try known patterns from previous Claude Code versions.
// When a Claude Code update breaks the build, find the new pattern with:
//   grep -o '.\{20\}platform.*win32.\{20\}' cli.js
// then add it to this list.
const knownPatterns = [
    'wT5=process.platform!=="win32"',  // Claude Code ~0.2.x (2025-02+)
    'fG5=process.platform!=="win32"',  // Claude Code earlier versions
    // Add future known patterns here as Claude Code updates change the variable name
];

for (const orig of knownPatterns) {
    if (!src.includes(orig)) {
        continue;
    }
    const count = src.split(orig).length - 1;
    if (count !== 1) {
        process.stderr.write('CCY PATCH WARNING: known pattern "' + orig + '" found ' + count + ' times (expected 1) - skipping\n');
        continue;
    }
    src = src.replace(orig, orig + SUSPEND_GUARD);
    fs.writeFileSync(CLI_PATH, src);
    process.stdout.write('CCY PATCH: ctrl+z suspend patch applied (known pattern: ' + JSON.stringify(orig) + ')\n');
    process.exit(0);
}

// Strategy 2: Dynamic discovery.
// The minified variable name changes between Claude Code versions.
// Step A: find the variable used in the ctrl+z call site: z.ctrl&&<VAR> near handleSuspend.
// Step B: find that variable's assignment: <VAR>=process.platform!=="win32".
// This is more reliable than searching near handleSuspend, because the assignment
// may be far from both the call site and the method definition.
const ctrlZMatch = src.match(/z\.name==="z"&&z\.ctrl&&(\w+)/);
if (ctrlZMatch) {
    const varName = ctrlZMatch[1];
    const orig = varName + '=process.platform!=="win32"';
    if (src.includes(orig)) {
        const count = src.split(orig).length - 1;
        if (count === 1) {
            src = src.replace(orig, orig + SUSPEND_GUARD);
            fs.writeFileSync(CLI_PATH, src);
            process.stdout.write('CCY PATCH: ctrl+z suspend patch applied (dynamic discovery: ' + JSON.stringify(orig) + ')\n');
            process.stdout.write('CCY PATCH INFO: Add "' + orig + '" to knownPatterns in ccy-ctrl-z-patch.js to avoid dynamic search next build\n');
            process.exit(0);
        } else {
            process.stderr.write('CCY PATCH WARNING: dynamic pattern "' + orig + '" found ' + count + ' times - too ambiguous to patch safely\n');
        }
    } else {
        process.stderr.write('CCY PATCH WARNING: found ctrl+z var "' + varName + '" but could not find its platform assignment\n');
    }
}

// Soft-fail: warn but don't break the build.
// The container is still usable; ctrl+z will just freeze it.
process.stderr.write('CCY PATCH WARNING: ctrl+z patch target not found - skipping (Claude Code internals changed)\n');
process.stderr.write('CCY PATCH INFO: ctrl+z may freeze the container. The CCY_DISABLE_SUSPEND env var will have no effect.\n');
process.stderr.write('CCY PATCH INFO: To fix: find new pattern near "handleSuspend" in cli.js and update ccy-ctrl-z-patch.js\n');
process.stderr.write('CCY PATCH INFO: Quick search: grep -o \'.\\{20\\}platform.*win32.\\{20\\}\' ' + CLI_PATH + '\n');
process.exit(0);
