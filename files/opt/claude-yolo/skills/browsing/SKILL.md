---
name: browsing
description: Use when you need to automate browser tasks — teaches agent-browser CLI for launching Chromium, navigating pages, extracting content, clicking elements, and filling forms
allowed-tools: Bash
---

# Browser Automation with agent-browser

## Overview

Use `agent-browser` to control Chromium for web automation tasks. It is a CLI tool that manages the full browser lifecycle — no external browser process needed.

**Announce:** "I'm using the browsing skill with agent-browser to automate Chrome."

## When to Use

- Navigating websites and extracting content
- Filling and submitting forms
- Taking screenshots of pages or elements
- Multi-step web workflows
- Acceptance testing of web applications

## Quick Reference

```bash
# Navigate and extract page content
agent-browser run "navigate https://example.com; extract text"

# Take a screenshot
agent-browser run "navigate https://example.com; screenshot /tmp/page.png"

# Click an element
agent-browser run "navigate https://example.com; click '#submit-button'"

# Fill a login form
agent-browser run "navigate https://example.com/login; type '#email' 'user@example.com'; type '#password' 'secret'; click 'button[type=submit]'"
```

## Commands

### Navigation

```bash
# Navigate to URL
agent-browser run "navigate https://example.com"

# Wait for element before proceeding
agent-browser run "navigate https://example.com; wait-for '.content-loaded'"

# Wait for text to appear
agent-browser run "navigate https://example.com; wait-text 'Welcome'"
```

### Content Extraction

```bash
# Extract page as markdown (best for reading content)
agent-browser run "navigate https://example.com; extract markdown"

# Extract plain text
agent-browser run "navigate https://example.com; extract text"

# Extract specific element only
agent-browser run "navigate https://example.com; extract text 'h1'"

# Get an attribute value
agent-browser run "navigate https://example.com; attr 'a.download' href"

# Execute JavaScript and return result
agent-browser run "navigate https://example.com; eval 'document.title'"
```

### Interaction

```bash
# Click element by CSS selector
agent-browser run "navigate https://example.com; click 'button.submit'"

# Type into input field
agent-browser run "navigate https://example.com; type '#search' 'my query'"

# Select dropdown option
agent-browser run "navigate https://example.com; select 'select[name=country]' 'GB'"
```

### Screenshots

```bash
# Full page screenshot
agent-browser run "navigate https://example.com; screenshot /tmp/page.png"

# Screenshot of specific element
agent-browser run "navigate https://example.com; screenshot /tmp/header.png '#header'"
```

## Common Patterns

### Login and Navigate

```bash
agent-browser run "
  navigate https://app.example.com/login;
  wait-for 'input[name=email]';
  type 'input[name=email]' 'user@example.com';
  type 'input[name=password]' 'secret';
  click 'button[type=submit]';
  wait-text 'Dashboard';
  extract markdown
"
```

### Scrape a List

```bash
agent-browser run "
  navigate https://example.com/products;
  wait-for '.product-list';
  extract text '.product-list'
"
```

### Multi-Step Form

```bash
agent-browser run "
  navigate https://example.com/checkout;
  type '#name' 'Test User';
  type '#email' 'test@example.com';
  select '#country' 'GB';
  click '#next-step';
  wait-for '#payment-section';
  screenshot /tmp/payment.png
"
```

## Tips

**Always wait before interaction** — pages need time to load:

```bash
# BAD — may fail if page is slow
navigate https://example.com; click '#button'

# GOOD — wait first
navigate https://example.com; wait-for '#button'; click '#button'
```

**Use specific CSS selectors** — avoid selectors that match multiple elements:

```bash
# BAD
click 'button'

# GOOD
click 'button[type=submit]'
click '#login-button'
```

**Chain commands with semicolons** — operations run in sequence in a single call.

**Inspect before interacting** — if unsure of selectors, extract HTML first:

```bash
agent-browser run "navigate https://example.com; extract html"
```

## Headless vs Headed Mode

The container defaults to headed mode with Wayland support (configured in `/root/.agent-browser/config.json`). Browser windows appear on the host desktop when Wayland is available.

For explicit headless operation:

```bash
agent-browser --headless run "navigate https://example.com; extract text"
```

## Troubleshooting

**Browser fails to start:**
- Try headless mode: `agent-browser --headless run "..."`
- Check display: `echo $WAYLAND_DISPLAY $DISPLAY`

**Element not found:**
- Use `extract html` to inspect the actual page structure
- Add `wait-for` before interacting with dynamic content

**Screenshot is blank:**
- Add `wait-for 'body'` before the screenshot step

## Further Reading

See [COMMANDLINE-USAGE.md](COMMANDLINE-USAGE.md) for the full command reference.
See [EXAMPLES.md](EXAMPLES.md) for more worked examples.
