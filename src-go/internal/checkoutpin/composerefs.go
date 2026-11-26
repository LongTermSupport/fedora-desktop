package checkoutpin

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// composeUse is what one compose file makes compose read beyond its own text (K7):
//
//   - follow: the compose files it includes or extends, judged and hashed as compose files;
//   - pinned: the other checkout files whose bytes compose reads (each build's Dockerfile, each
//     env_file and label_file, each include's env_file, each config's and secret's file),
//     hashed by name, presence and content;
//   - linkFree: the checkout paths compose hands the engine by name (bind sources, build
//     contexts, additional contexts, config and secret files, a bind volume's device), none of
//     whose parts may be a link, since a link redirects them without changing any byte hashed.
//
// Each path is as written, relative to the compose file's directory unless absolute.
type composeUse struct {
	follow, pinned, linkFree []string
	// outsideOK are linkFree paths that may lie outside the checkout: a bind or a bound file
	// names a host path the person reads in the compose text, which no link of the checkout
	// can move.
	outsideOK map[string]bool
}

// composeTopKeys are the compose file's top-level keys; any other, but an x- extension, is
// refused, since ccy cannot tell whether it reads a file.
var composeTopKeys = []string{"version", "name", "services", "networks", "volumes", "configs", "secrets", "include", "models"}

// composeServiceKeys are a service's keys that read no host file, or whose file reads are
// judged below (build, env_file, label_file, volumes, extends, credential_spec). Any other, but
// an x- extension, is refused.
var composeServiceKeys = []string{
	"annotations", "attach", "blkio_config", "build", "cap_add", "cap_drop", "cgroup", "cgroup_parent", "command",
	"configs", "container_name", "cpu_count", "cpu_percent", "cpu_period", "cpu_quota", "cpu_rt_period",
	"cpu_rt_runtime", "cpu_shares", "cpus", "cpuset", "credential_spec", "depends_on", "deploy", "develop",
	"device_cgroup_rules", "devices", "dns", "dns_opt", "dns_search", "domainname", "driver_opts", "entrypoint",
	"env_file", "environment", "expose", "extends", "external_links", "extra_hosts", "gpus", "group_add",
	"healthcheck", "hostname", "image", "init", "ipc", "isolation", "label_file", "labels", "links", "logging",
	"mac_address", "mem_limit", "mem_reservation", "mem_swappiness", "memswap_limit", "models", "network_mode",
	"networks", "oom_kill_disable", "oom_score_adj", "pid", "pids_limit", "platform", "ports", "post_start",
	"pre_stop", "privileged", "profiles", "provider", "pull_policy", "read_only", "restart", "runtime", "scale",
	"secrets", "security_opt", "shm_size", "stdin_open", "stop_grace_period", "stop_signal", "storage_opt",
	"sysctls", "tmpfs", "tty", "ulimits", "use_api_socket", "user", "userns_mode", "uts", "volumes",
	"volumes_from", "working_dir",
}

// composeUses parses one compose file (anchors, aliases and merge keys resolved) into what it
// makes compose read.
func composeUses(content []byte) (composeUse, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return composeUse{}, fmt.Errorf("not readable as compose YAML: %w", err)
	}
	u := composeUse{outsideOK: map[string]bool{}}
	if err := unknownKeys(doc, composeTopKeys, "the compose file"); err != nil {
		return u, err
	}
	for _, step := range []func(map[string]any) error{u.includes, u.services, u.topFiles, u.topVolumes} {
		if err := step(doc); err != nil {
			return u, err
		}
	}
	return u, nil
}

// unknownKeys refuses a key of m that is neither one of known nor an x- extension.
func unknownKeys(m map[string]any, known []string, where string) error {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !slices.Contains(known, k) && !strings.HasPrefix(k, "x-") {
			return fmt.Errorf("%s has the key %q, which ccy does not know: it cannot tell whether it makes compose read a file no approval covers", where, k)
		}
	}
	return nil
}

func (u *composeUse) includes(doc map[string]any) error {
	list, present := doc["include"]
	if !present {
		return nil
	}
	items, isList := list.([]any)
	if !isList {
		return errors.New("include is not a list")
	}
	for _, item := range items {
		switch it := item.(type) {
		case string:
			u.follow = append(u.follow, it)
		case map[string]any:
			for key, into := range map[string]*[]string{"path": &u.follow, "env_file": &u.pinned, "project_directory": &u.linkFree} {
				paths, err := strings1(it[key], "an include's "+key)
				if err != nil {
					return err
				}
				*into = append(*into, paths...)
			}
		default:
			return errors.New("an include entry is neither a path nor a map")
		}
	}
	return nil
}

func (u *composeUse) services(doc map[string]any) error {
	if doc["services"] == nil {
		return nil
	}
	services, isMap := doc["services"].(map[string]any)
	if !isMap {
		return errors.New("services is not a map")
	}
	for _, name := range slices.Sorted(maps.Keys(services)) {
		if services[name] == nil {
			continue
		}
		svc, isMap := services[name].(map[string]any)
		if !isMap {
			return fmt.Errorf("service %s is not a map", name)
		}
		if err := u.service(name, svc); err != nil {
			return fmt.Errorf("service %s: %w", name, err)
		}
	}
	return nil
}

func (u *composeUse) service(name string, svc map[string]any) error {
	if err := unknownKeys(svc, composeServiceKeys, "service "+name); err != nil {
		return err
	}
	if spec, isMap := svc["credential_spec"].(map[string]any); isMap && spec["file"] != nil {
		return errors.New("credential_spec names a file, which ccy does not judge")
	}
	if ext, isMap := svc["extends"].(map[string]any); isMap {
		if file, isString := ext["file"].(string); isString && file != "" {
			u.follow = append(u.follow, file)
		}
	}
	for _, key := range []string{"env_file", "label_file"} {
		files, err := envFiles(svc[key], key)
		if err != nil {
			return err
		}
		u.pinned = append(u.pinned, files...)
	}
	if err := u.build(svc["build"]); err != nil {
		return err
	}
	return u.volumes(svc["volumes"])
}

// envFiles is an env_file or label_file: a path, or a list of paths or of maps with a path.
func envFiles(v any, key string) ([]string, error) {
	list, isList := v.([]any)
	if !isList {
		return strings1(v, key)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			item = m["path"]
		}
		p, isString := item.(string)
		if !isString {
			return nil, fmt.Errorf("an entry of %s is not a path", key)
		}
		out = append(out, p)
	}
	return out, nil
}

// build is a service's build: a context path, or a map of context, dockerfile and
// additional_contexts. A remote context (a URL) is read from no checkout.
func (u *composeUse) build(v any) error {
	if v == nil {
		return nil
	}
	context, dockerfile, extra := ".", "Dockerfile", []string{}
	switch b := v.(type) {
	case string:
		context = b
	case map[string]any:
		if c, isString := b["context"].(string); isString {
			context = c
		}
		if d, isString := b["dockerfile"].(string); isString {
			dockerfile = d
		}
		var err error
		if extra, err = additionalContexts(b["additional_contexts"]); err != nil {
			return err
		}
	default:
		return errors.New("build is neither a path nor a map")
	}
	if !remote(context) {
		u.linkFree = append(u.linkFree, context)
		if !path.IsAbs(dockerfile) {
			dockerfile = path.Join(context, dockerfile)
		}
		u.pinned = append(u.pinned, dockerfile)
	}
	for _, s := range extra {
		if !remote(s) {
			u.linkFree = append(u.linkFree, s)
		}
	}
	return nil
}

// additionalContexts are a build's additional contexts' values, from a map or a list of
// name=value.
func additionalContexts(v any) ([]string, error) {
	var out []string
	switch m := v.(type) {
	case nil:
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(m)) {
			s, isString := m[k].(string)
			if !isString {
				return nil, errors.New("an additional context is not a string")
			}
			out = append(out, s)
		}
	case []any:
		for _, item := range m {
			s, isString := item.(string)
			_, val, found := strings.Cut(s, "=")
			if !isString || !found {
				return nil, errors.New("an additional context is not name=value")
			}
			out = append(out, val)
		}
	default:
		return nil, errors.New("additional_contexts is neither a map nor a list")
	}
	return out, nil
}

// remote is a build context compose fetches rather than reads from disk.
func remote(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "service:") || strings.HasPrefix(s, "target:")
}

// volumes are a service's mounts: the source of each bind, short or long syntax.
func (u *composeUse) volumes(v any) error {
	if v == nil {
		return nil
	}
	list, isList := v.([]any)
	if !isList {
		return errors.New("volumes is not a list")
	}
	for _, item := range list {
		switch it := item.(type) {
		case string:
			src := strings.SplitN(it, ":", 2)[0]
			if strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") || startsAtHome(src) || strings.Contains(src, "$") {
				u.bind(src)
			}
		case map[string]any:
			if it["type"] != "bind" {
				continue
			}
			src, isString := it["source"].(string)
			if !isString {
				return errors.New("a bind mount has no source")
			}
			u.bind(src)
		default:
			return errors.New("a volumes entry is neither a string nor a map")
		}
	}
	return nil
}

func (u *composeUse) bind(src string) {
	u.linkFree = append(u.linkFree, src)
	u.outsideOK[src] = true
}

// topFiles are the top-level configs' and secrets' files: bound into a container, so link-free
// in the checkout, and hashed.
func (u *composeUse) topFiles(doc map[string]any) error {
	for _, key := range []string{"configs", "secrets"} {
		entries, isMap := doc[key].(map[string]any)
		if !isMap {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(entries)) {
			entry, isMap := entries[name].(map[string]any)
			if !isMap || entry["file"] == nil {
				continue
			}
			file, isString := entry["file"].(string)
			if !isString {
				return fmt.Errorf("%s %s: file is not a path", key, name)
			}
			u.bind(file)
			u.pinned = append(u.pinned, file)
		}
	}
	return nil
}

// topVolumes are the named volumes a local driver binds from a host path (o: bind, device).
func (u *composeUse) topVolumes(doc map[string]any) error {
	volumes, isMap := doc["volumes"].(map[string]any)
	if !isMap {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(volumes)) {
		vol, isMap := volumes[name].(map[string]any)
		if !isMap {
			continue
		}
		opts, isMap := vol["driver_opts"].(map[string]any)
		if !isMap {
			continue
		}
		o, oString := opts["o"].(string)
		device, isPath := opts["device"].(string)
		if oString && isPath && strings.Contains(o, "bind") {
			u.bind(device)
		}
	}
	return nil
}

// strings1 is a value that is absent, one string or a list of strings.
func strings1(v any, what string) ([]string, error) {
	switch s := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{s}, nil
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			str, isString := item.(string)
			if !isString {
				return nil, fmt.Errorf("%s is neither a string nor a list of strings", what)
			}
			out = append(out, str)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s is neither a string nor a list of strings", what)
}
