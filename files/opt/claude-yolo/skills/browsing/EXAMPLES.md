# agent-browser Examples

Worked examples for common browser automation scenarios using `agent-browser`.

## Table of Contents

1. [Basic Content Extraction](#basic-content-extraction)
2. [Login Flows](#login-flows)
3. [Form Automation](#form-automation)
4. [Web Scraping](#web-scraping)
5. [Screenshots](#screenshots)
6. [Inspecting Unknown Pages](#inspecting-unknown-pages)
7. [Dynamic Content](#dynamic-content)
8. [Multi-Step Workflows](#multi-step-workflows)

---

## Basic Content Extraction

### Get page title and heading

```bash
agent-browser run "
  navigate https://example.com;
  eval 'document.title'
"
```

```bash
agent-browser run "
  navigate https://example.com;
  extract text 'h1'
"
```

### Read article content as markdown

```bash
agent-browser run "
  navigate https://example.com/blog/article;
  wait-for 'article';
  extract markdown 'article'
"
```

### Extract all links from a page

```bash
agent-browser run "
  navigate https://example.com;
  eval 'Array.from(document.querySelectorAll(\"a[href]\")).map(a => a.href).join(\"\\n\")'
"
```

### Get a download URL

```bash
agent-browser run "
  navigate https://example.com/downloads;
  attr 'a.download-btn' href
"
```

---

## Login Flows

### Basic username/password login

```bash
agent-browser run "
  navigate https://app.example.com/login;
  wait-for 'input[name=email]';
  type 'input[name=email]' 'user@example.com';
  type 'input[name=password]' 'mypassword';
  click 'button[type=submit]';
  wait-text 'Dashboard';
  extract markdown
"
```

### Login then navigate to a specific page

```bash
agent-browser run "
  navigate https://app.example.com/login;
  wait-for '#username';
  type '#username' 'admin';
  type '#password' 'secret';
  click '#sign-in';
  wait-for '.user-menu';
  navigate https://app.example.com/settings;
  wait-for '.settings-panel';
  extract markdown '.settings-panel'
"
```

### Login and capture post-login state

```bash
agent-browser run "
  navigate https://app.example.com/login;
  wait-for 'form.login';
  type 'input[type=email]' 'user@example.com';
  type 'input[type=password]' 'secret';
  click 'input[type=submit]';
  wait-text 'Welcome back';
  screenshot /tmp/logged-in.png
"
```

---

## Form Automation

### Fill and submit a contact form

```bash
agent-browser run "
  navigate https://example.com/contact;
  wait-for 'form#contact';
  type 'input[name=name]' 'Test User';
  type 'input[name=email]' 'test@example.com';
  type 'textarea[name=message]' 'Hello, this is a test message.';
  click 'button[type=submit]';
  wait-text 'Thank you';
  extract text '.confirmation'
"
```

### Multi-page checkout form

```bash
agent-browser run "
  navigate https://example.com/checkout;
  wait-for '#billing-form';
  type '#first-name' 'Test';
  type '#last-name' 'User';
  type '#email' 'test@example.com';
  type '#address' '123 Test Street';
  select '#country' 'GB';
  click '#continue';
  wait-for '#shipping-form';
  screenshot /tmp/shipping-step.png
"
```

### Select from dropdown then submit

```bash
agent-browser run "
  navigate https://example.com/filter;
  wait-for 'select[name=category]';
  select 'select[name=category]' 'electronics';
  select 'select[name=sort]' 'price-asc';
  click 'button[type=submit]';
  wait-for '.results-list';
  extract text '.results-list'
"
```

---

## Web Scraping

### Scrape a product listing

```bash
agent-browser run "
  navigate https://example.com/products;
  wait-for '.product-grid';
  extract text '.product-grid'
"
```

### Scrape a table

```bash
agent-browser run "
  navigate https://example.com/data;
  wait-for 'table';
  extract html 'table'
"
```

### Scrape paginated results (first page)

```bash
agent-browser run "
  navigate https://example.com/listings?page=1;
  wait-for '.listings';
  extract markdown '.listings'
"
```

### Get structured data via JavaScript

```bash
agent-browser run "
  navigate https://example.com/products;
  wait-for '.product-card';
  eval 'Array.from(document.querySelectorAll(\".product-card\")).map(el => ({ name: el.querySelector(\".name\")?.textContent, price: el.querySelector(\".price\")?.textContent })).map(JSON.stringify).join(\"\\n\")'
"
```

### Extract all image URLs

```bash
agent-browser run "
  navigate https://example.com/gallery;
  wait-for '.gallery';
  eval 'Array.from(document.querySelectorAll(\".gallery img\")).map(img => img.src).join(\"\\n\")'
"
```

---

## Screenshots

### Full page screenshot

```bash
agent-browser run "
  navigate https://example.com;
  wait-for 'body';
  screenshot /tmp/page.png
"
```

### Screenshot specific section

```bash
agent-browser run "
  navigate https://example.com;
  wait-for '#hero-section';
  screenshot /tmp/hero.png '#hero-section'
"
```

### Screenshot after interaction

```bash
agent-browser run "
  navigate https://example.com/menu;
  wait-for '.nav-menu';
  click '.nav-menu .dropdown-toggle';
  wait-for '.dropdown-menu';
  screenshot /tmp/dropdown-open.png '.nav-menu'
"
```

### Capture before and after

```bash
# Before state
agent-browser run "
  navigate https://example.com/widget;
  wait-for '#widget';
  screenshot /tmp/before.png '#widget'
"

# After interaction
agent-browser run "
  navigate https://example.com/widget;
  wait-for '#widget';
  click '#widget .toggle';
  wait-for '#widget.expanded';
  screenshot /tmp/after.png '#widget'
"
```

---

## Inspecting Unknown Pages

When you don't know the page structure, extract HTML first to find selectors.

### Discover page structure

```bash
agent-browser run "
  navigate https://example.com;
  extract html
"
```

### Inspect a specific section

```bash
agent-browser run "
  navigate https://example.com;
  extract html 'nav'
"
```

### Find form field names

```bash
agent-browser run "
  navigate https://example.com/login;
  extract html 'form'
"
```

Then use the discovered attributes to write your interaction commands.

---

## Dynamic Content

### Wait for async content to load

```bash
agent-browser run "
  navigate https://example.com/feed;
  wait-for '.feed-item';
  extract text '.feed-container'
"
```

### Wait for text confirmation

```bash
agent-browser run "
  navigate https://example.com/search;
  type '#search-input' 'test query';
  click '#search-btn';
  wait-text 'results found';
  extract text '.results'
"
```

### Handle redirect after form submission

```bash
agent-browser run "
  navigate https://example.com/signup;
  wait-for 'form#signup';
  type 'input[name=email]' 'new@example.com';
  type 'input[name=password]' 'password123';
  click 'button[type=submit]';
  wait-for '.welcome-page';
  extract text 'h1'
"
```

### Get dynamically loaded attribute

```bash
agent-browser run "
  navigate https://example.com/player;
  wait-for 'video[src]';
  attr 'video' src
"
```

---

## Multi-Step Workflows

### Search and extract result details

```bash
agent-browser run "
  navigate https://example.com/search;
  wait-for '#search-box';
  type '#search-box' 'my search term';
  click '#search-submit';
  wait-for '.search-results';
  extract markdown '.search-results'
"
```

### Navigate through a wizard

```bash
agent-browser run "
  navigate https://example.com/wizard;
  wait-for '#step-1';
  type '#name' 'Test User';
  click '#next';
  wait-for '#step-2';
  select '#plan' 'basic';
  click '#next';
  wait-for '#step-3';
  screenshot /tmp/wizard-step-3.png
"
```

### Acceptance test: verify page content

```bash
agent-browser run "
  navigate https://localhost:3000;
  wait-for 'h1';
  extract text 'h1'
"
# Output should contain expected heading text

agent-browser run "
  navigate https://localhost:3000/about;
  wait-for '.about-section';
  extract text '.about-section'
"
```

### Acceptance test: verify form submission

```bash
agent-browser run "
  navigate https://localhost:3000/contact;
  wait-for 'form';
  type 'input[name=email]' 'test@example.com';
  type 'textarea[name=message]' 'Test message';
  click 'button[type=submit]';
  wait-text 'Message sent';
  screenshot /tmp/success-state.png
"
```

---

## Tips

**Inspect before interacting** — when selectors are unknown:
```bash
agent-browser run "navigate https://example.com; extract html 'form'"
```

**Always wait on dynamic pages** — add `wait-for` before clicks and extractions:
```bash
# Without wait — may fail on slow pages
agent-browser run "navigate https://example.com; click '#button'"

# With wait — reliable
agent-browser run "navigate https://example.com; wait-for '#button'; click '#button'"
```

**Use headless for speed** — when visual output isn't needed:
```bash
agent-browser --headless run "navigate https://example.com; extract text"
```

---

## See Also

- [SKILL.md](SKILL.md) — Overview and quick start
- [COMMANDLINE-USAGE.md](COMMANDLINE-USAGE.md) — Full command reference
