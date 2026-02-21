# agent-browser Command Reference

Full command reference for `agent-browser` — the CLI tool for Chromium browser automation.

## CLI Syntax

```
agent-browser [OPTIONS] run "<commands>"
agent-browser [OPTIONS] install
```

Commands are passed as a single quoted string, separated by semicolons. They execute in sequence.

## Global Options

| Option | Description |
|--------|-------------|
| `--headless` | Force headless mode (no visible browser window) |
| `--help` | Show help |

**Default behaviour**: The container is configured for headed mode with Wayland support. Browser windows appear on the host desktop when Wayland is available. See `/root/.agent-browser/config.json`.

## Commands

### `navigate <url>`

Navigate to a URL. Waits for the page to finish loading.

```bash
agent-browser run "navigate https://example.com"
agent-browser run "navigate https://example.com/path?query=value"
```

Always the first command in a sequence. Subsequent navigation mid-sequence is supported.

---

### `wait-for '<selector>'`

Wait for a CSS selector to appear in the DOM before continuing.

```bash
agent-browser run "navigate https://example.com; wait-for '.content'"
agent-browser run "navigate https://example.com; wait-for '#login-form'"
agent-browser run "navigate https://example.com; wait-for 'input[name=email]'"
```

Use before interacting with elements on dynamic pages. Prevents failures when content loads asynchronously.

---

### `wait-text '<text>'`

Wait for specific text to appear anywhere on the page.

```bash
agent-browser run "navigate https://example.com; wait-text 'Welcome'"
agent-browser run "navigate https://example.com; wait-text 'Loading complete'"
```

Useful after form submissions or navigation where you expect a specific confirmation message.

---

### `extract text ['<selector>']`

Extract plain text content from the page or a specific element.

```bash
# Entire page
agent-browser run "navigate https://example.com; extract text"

# Specific element
agent-browser run "navigate https://example.com; extract text 'h1'"
agent-browser run "navigate https://example.com; extract text '.product-description'"
agent-browser run "navigate https://example.com; extract text 'table.results'"
```

---

### `extract markdown ['<selector>']`

Extract page content converted to Markdown. Best for reading article or documentation content.

```bash
# Entire page as markdown
agent-browser run "navigate https://example.com; extract markdown"

# Specific section as markdown
agent-browser run "navigate https://example.com; extract markdown 'article'"
agent-browser run "navigate https://example.com; extract markdown '#main-content'"
```

Preserves headings, links, lists, and code blocks. Preferred over `extract text` when content structure matters.

---

### `extract html ['<selector>']`

Extract raw HTML source. Use to inspect page structure when CSS selectors are unknown.

```bash
# Full page HTML
agent-browser run "navigate https://example.com; extract html"

# Specific element HTML
agent-browser run "navigate https://example.com; extract html '#navigation'"
agent-browser run "navigate https://example.com; extract html 'form.login'"
```

Useful for debugging selector issues before writing interaction commands.

---

### `attr '<selector>' <attribute>`

Get the value of an HTML attribute from an element.

```bash
agent-browser run "navigate https://example.com; attr 'a.download' href"
agent-browser run "navigate https://example.com; attr 'img.hero' src"
agent-browser run "navigate https://example.com; attr 'input[name=csrf]' value"
agent-browser run "navigate https://example.com; attr 'meta[name=description]' content"
```

Returns the attribute value as output.

---

### `eval '<javascript>'`

Execute JavaScript in the page context and return the result.

```bash
agent-browser run "navigate https://example.com; eval 'document.title'"
agent-browser run "navigate https://example.com; eval 'document.querySelectorAll(\"li\").length'"
agent-browser run "navigate https://example.com; eval 'window.location.href'"
agent-browser run "navigate https://example.com; eval 'localStorage.getItem(\"token\")'"
```

Output is the return value of the expression. For complex operations, use a self-invoking function:

```bash
agent-browser run "navigate https://example.com; eval '(function(){ return document.querySelectorAll(\".item\").length; })()'"
```

---

### `click '<selector>'`

Click an element matched by CSS selector.

```bash
agent-browser run "navigate https://example.com; click 'button[type=submit]'"
agent-browser run "navigate https://example.com; click '#login-button'"
agent-browser run "navigate https://example.com; click 'a[href=\"/dashboard\"]'"
agent-browser run "navigate https://example.com; click '.nav-menu li:first-child a'"
```

Always use specific selectors. Prefer `id` attributes (`#id`) or `type` attributes over generic tag names.

---

### `type '<selector>' '<text>'`

Type text into an input field. Clears the field first.

```bash
agent-browser run "navigate https://example.com; type '#search' 'my query'"
agent-browser run "navigate https://example.com; type 'input[name=email]' 'user@example.com'"
agent-browser run "navigate https://example.com; type 'textarea#message' 'Hello world'"
```

---

### `select '<selector>' '<value>'`

Select an option from a `<select>` dropdown by option value.

```bash
agent-browser run "navigate https://example.com; select 'select[name=country]' 'GB'"
agent-browser run "navigate https://example.com; select '#size-picker' 'large'"
agent-browser run "navigate https://example.com; select 'select.year' '2025'"
```

The value must match the `value` attribute of the `<option>` element, not its display text.

---

### `screenshot <path> ['<selector>']`

Capture a screenshot and save to the given path.

```bash
# Full page screenshot
agent-browser run "navigate https://example.com; screenshot /tmp/page.png"

# Screenshot of specific element
agent-browser run "navigate https://example.com; screenshot /tmp/header.png '#header'"
agent-browser run "navigate https://example.com; screenshot /tmp/table.png 'table.results'"
```

Always save screenshots to `/tmp/`. Add `wait-for 'body'` before the screenshot step if the page may not be fully rendered.

---

## Chaining Commands

Commands are separated by semicolons and run in sequence within a single browser session:

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

The browser stays open and maintains state (cookies, session) between commands in the same call.

---

## Headless Mode

Override the default headed configuration:

```bash
agent-browser --headless run "navigate https://example.com; extract text"
```

Use when:
- Running in environments without Wayland or X11
- Speed is preferred over visual inspection
- CI/CD or automated testing scenarios

---

## Configuration

The container ships a pre-configured `/root/.agent-browser/config.json` enabling headed Wayland mode:

```json
{
  "headed": true,
  "args": [
    "--enable-features=UseOzonePlatform",
    "--ozone-platform=wayland",
    "--no-sandbox",
    "--disable-gpu",
    "--disable-software-rasterizer",
    "--disable-dev-shm-usage"
  ]
}
```

Modify this file to permanently change browser launch arguments.

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `command not found: agent-browser` | Rebuild container image — `agent-browser` is installed at build time |
| Browser fails to start | Try `--headless` flag; check `echo $WAYLAND_DISPLAY $DISPLAY` |
| Element not found | Use `extract html` to inspect actual DOM structure |
| Selector matches wrong element | Add more specificity: `form#login input[name=email]` |
| Interaction fails on dynamic content | Add `wait-for '<selector>'` before the interaction |
| Screenshot is blank | Add `wait-for 'body'` before the `screenshot` step |
| Text not appearing | Use `wait-text` to synchronise with async content |

---

## See Also

- [SKILL.md](SKILL.md) — Overview and quick reference
- [EXAMPLES.md](EXAMPLES.md) — Worked examples for common scenarios
