package checkoutpin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// digestOf is ComposeDigest's digest, or the test fails.
func digestOf(t *testing.T, dir string) string {
	t.Helper()
	_, digest, _, err := ComposeDigest(dir)
	if err != nil {
		t.Fatalf("ComposeDigest: %v", err)
	}
	return digest
}

// P6 (finding 5, the hazard): the compose digest covers every checkout file compose reads
// beyond the compose files: each service's Dockerfile, its env_file and label_file, each
// include's env_file, and each config's and secret's file. A change to any of them changes the
// digest.
func TestComposeDigestCoversWhatComposeReads(t *testing.T) {
	dir := checkout(t)
	write(t, dir, "compose.yml", `include:
  - path: inc.yml
    env_file: inc.env
services:
  app:
    build:
      context: ./app
      dockerfile: Containerfile
    env_file: [app.env, {path: opt.env, required: false}]
    label_file: labels
  db:
    build: ./db
    env_file: db.env
configs:
  conf: {file: ./conf/app.ini}
secrets:
  key: {file: secret.txt}
`)
	for name, content := range map[string]string{
		"inc.yml": "services: {}\n", "inc.env": "A=1\n", "app/Containerfile": "FROM x\n", "app.env": "B=1\n",
		"labels": "l=1\n", "db/Dockerfile": "FROM y\n", "db.env": "C=1\n", "conf/app.ini": "x\n", "secret.txt": "s\n",
	} {
		write(t, dir, name, content)
	}
	files, base, _, err := ComposeDigest(dir)
	if err != nil {
		t.Fatalf("ComposeDigest: %v", err)
	}
	for _, want := range []string{"app/Containerfile", "db/Dockerfile", "app.env", "labels", "inc.env", "conf/app.ini", "secret.txt"} {
		if !strings.Contains(strings.Join(files, ","), want) {
			t.Errorf("files %v lack %s", files, want)
		}
	}
	for _, name := range []string{"app/Containerfile", "db/Dockerfile", "app.env", "opt.env", "labels", "db.env", "inc.env", "conf/app.ini", "secret.txt"} {
		write(t, dir, name, "changed "+name+"\n")
		if now := digestOf(t, dir); now == base {
			t.Errorf("%s changed and the digest did not", name)
		} else {
			base = now
		}
	}
}

// P7 (finding 5, the hazard): a bind source, a build context, or a config or secret file in the
// checkout is refused when any part of its path is a link, since a container can make one point
// at any host path and the digest would not change. A link elsewhere, and a source outside the
// checkout (shown to the person in the compose text), are not refused.
func TestComposeRefusesLinkedCheckoutPaths(t *testing.T) {
	outside := t.TempDir()
	for name, content := range map[string]string{
		"short bind":   "services:\n  a:\n    volumes: [./data:/d]\n",
		"bind under":   "services:\n  a:\n    volumes: [\"./data/sub:/d:ro\"]\n",
		"long bind":    "services:\n  a:\n    volumes:\n      - type: bind\n        source: ./data\n        target: /d\n",
		"cwd bind":     "services:\n  a:\n    volumes: [\"${PWD}/data:/d\"]\n",
		"build":        "services:\n  a:\n    build: ./data\n",
		"context":      "services:\n  a:\n    build: {context: data}\n",
		"extra ctx":    "services:\n  a:\n    build: {context: ., additional_contexts: {x: ./data}}\n",
		"config":       "services: {}\nconfigs:\n  c: {file: ./data/c}\n",
		"volume dev":   "services: {}\nvolumes:\n  v:\n    driver_opts: {type: none, o: bind, device: ./data}\n",
		"abs checkout": "services:\n  a:\n    volumes: [\"CHECKOUT/data:/d\"]\n",
	} {
		dir := checkout(t)
		if err := os.Symlink(outside, filepath.Join(dir, "data")); err != nil {
			t.Fatal(err)
		}
		write(t, dir, "compose.yml", strings.ReplaceAll(content, "CHECKOUT", dir))
		if _, _, _, err := ComposeDigest(dir); err == nil || !strings.Contains(err.Error(), "link") {
			t.Errorf("%s: a linked path was accepted (%v)", name, err)
		}
	}
	dir := checkout(t)
	if err := os.Symlink(outside, filepath.Join(dir, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "compose.yml", "services:\n  a:\n    volumes: [./data:/d, \""+outside+":/o\", named:/n, /srv/x:/x]\n    build: .\nvolumes: {named: {}}\n")
	if _, _, _, err := ComposeDigest(dir); err != nil {
		t.Errorf("plain paths refused: %v", err)
	}
}

// P8 (finding 5): what the digest cannot judge is refused, naming it: a key compose may read a
// file by that ccy does not know, a bind source or file path interpolated from a variable other
// than PWD, and a file compose reads from outside the checkout by a relative path.
func TestComposeRefusesWhatItCannotJudge(t *testing.T) {
	for name, c := range map[string]struct{ content, want string }{
		"unknown service key": {"services:\n  a:\n    future_file: x\n", "future_file"},
		"unknown top key":     {"services: {}\nfuture: {}\n", "future"},
		"interpolated bind":   {"services:\n  a:\n    volumes: [\"${DATA}:/d\"]\n", "${DATA}"},
		"interpolated env":    {"services:\n  a:\n    env_file: ${ENV}\n", "${ENV}"},
		"env outside":         {"services:\n  a:\n    env_file: ../x.env\n", "outside the checkout"},
		"dockerfile outside":  {"services:\n  a:\n    build: {context: ., dockerfile: ../Dockerfile}\n", "outside the checkout"},
		"credential spec":     {"services:\n  a:\n    credential_spec: {file: x.json}\n", "credential_spec"},
	} {
		dir := checkout(t)
		write(t, dir, "compose.yml", c.content)
		if _, _, _, err := ComposeDigest(dir); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, c.want)
		}
	}
	dir := checkout(t)
	write(t, dir, "compose.yml", "x-common: &c {image: x}\nname: p\nservices:\n  a:\n    <<: *c\n    x-podman: {in_pod: false}\n    environment: {A: \"${A}\"}\n    ports: [\"80:80\"]\n")
	if _, _, _, err := ComposeDigest(dir); err != nil {
		t.Errorf("known keys, an extension and a merge key refused: %v", err)
	}
}

// P9: a compose file whose shapes are not compose's is refused, never read as nothing; the list
// and map forms compose accepts are read.
func TestComposeShapes(t *testing.T) {
	for name, content := range map[string]string{
		"include map":     "include: {a: b}\n",
		"include item":    "include: [1]\n",
		"include path":    "include: [{path: {a: b}}]\n",
		"services list":   "services: [a]\n",
		"service scalar":  "services: {a: 1}\n",
		"env entry":       "services: {a: {env_file: [1]}}\n",
		"env map":         "services: {a: {env_file: {a: b}}}\n",
		"build scalar":    "services: {a: {build: 1}}\n",
		"extra map value": "services: {a: {build: {additional_contexts: {x: 1}}}}\n",
		"extra list item": "services: {a: {build: {additional_contexts: [x]}}}\n",
		"extra scalar":    "services: {a: {build: {additional_contexts: x}}}\n",
		"volumes map":     "services: {a: {volumes: {a: b}}}\n",
		"volume item":     "services: {a: {volumes: [1]}}\n",
		"bind no source":  "services: {a: {volumes: [{type: bind}]}}\n",
		"config file":     "services: {}\nconfigs: {c: {file: 1}}\n",
		"env list item":   "include: [{path: a.yml, env_file: [1]}]\n",
		"env in home":     "services: {a: {env_file: \"~/x.env\"}}\n",
	} {
		dir := checkout(t)
		write(t, dir, "compose.yml", content)
		if _, _, _, err := ComposeDigest(dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	dir := checkout(t)
	write(t, dir, "compose.yml", "services:\n  a:\n    build: {context: ., additional_contexts: [\"x=./ctx\", \"img=docker-image://x\"]}\n    env_file: [{path: e.env}]\n    volumes: [{type: volume, source: v, target: /v}]\n  b:\n  c:\n    build: https://example.com/r.git\nvolumes: {v: {}, w: {driver_opts: {o: bind, device: /srv/w}}, n: 1}\nconfigs: {c: {external: true}, d: 1}\n")
	if _, _, _, err := ComposeDigest(dir); err != nil {
		t.Errorf("compose's own forms refused: %v", err)
	}
}
