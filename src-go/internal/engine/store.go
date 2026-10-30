package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// store.go is what the container-images command (Plan 00235) asks of the engine's store: its
// images and containers read from podman's own JSON lists, the removals, and the two questions
// ccy's self-prune asks, which use only what podman and docker both print. The lists never ask
// `podman system df`, which fails on a store holding a volume owned outside the user's subuid
// range.

// inspect is the engine verb that answers about one or more named objects.
const inspect = "inspect"

// ErrPodmanOnly is a store question only podman answers (JSON lists, external containers).
var ErrPodmanOnly = errors.New("only podman's store is read this way")

// StoreImage is one image of `podman images --format json`.
type StoreImage struct {
	ID     string // the full id, without "sha256:"
	Names  []string
	Size   int64
	Labels map[string]string
	// Containers is podman's own count of the containers using the image.
	Containers int
	Created    time.Time
}

// StoreContainer is one container of `podman ps --all --external --format json`.
type StoreContainer struct {
	ID      string
	Name    string
	ImageID string
	Created time.Time
	State   string
	// External is a container another tool made in podman's store (buildah's working
	// containers): podman lists it in the "storage" state.
	External bool
	Command  string // the first word of its command ("buildah" for buildah's)
}

// Tag is one tag of a repository and the id of the image it names.
type Tag struct {
	Name string
	ID   string
}

// storeImage and storeContainer are podman's JSON, matched by field name in any case.
type storeImage struct {
	ID         string
	Names      []string
	Size       int64
	Labels     map[string]string
	Containers int
	Created    int64
}

type storeContainer struct {
	ID      string
	Names   []string
	ImageID string
	Created created
	State   string
	Command []string
}

// created is a container's creation time: podman 4 and 5 write an RFC 3339 time, podman 3 wrote
// Unix seconds.
type created struct{ time.Time }

func (c *created) UnmarshalJSON(b []byte) error {
	var secs int64
	if err := json.Unmarshal(b, &secs); err == nil {
		c.Time = time.Unix(secs, 0)
		return nil
	}
	var text string
	if err := json.Unmarshal(b, &text); err != nil {
		return fmt.Errorf("a created time is neither seconds nor a time: %s", b)
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return fmt.Errorf("a created time is not an RFC 3339 time: %w", err)
	}
	c.Time = t
	return nil
}

func bareID(id string) string { return strings.TrimPrefix(strings.TrimSpace(id), "sha256:") }

// podmanList runs a podman list and decodes its JSON.
func podmanList[T any](ctx context.Context, e Engine, what string, args ...string) ([]T, error) {
	if err := e.known(); err != nil {
		return nil, err
	}
	if e.kind != Podman {
		return nil, fmt.Errorf("%s of %s: %w", what, e.kind, ErrPodmanOnly)
	}
	res, err := e.run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", what, err)
	}
	var out []T
	if err := json.Unmarshal(bytes.TrimSpace(res.Stdout), &out); err != nil {
		return nil, fmt.Errorf("list %s: podman's answer is not a JSON list: %w", what, err)
	}
	return out, nil
}

// StoreImages is every image podman lists (not the intermediate ones), or only the dangling
// ones: untagged, with no image built on them.
func (e Engine) StoreImages(ctx context.Context, dangling bool) ([]StoreImage, error) {
	args := []string{"images"}
	if dangling {
		args = append(args, "--filter", "dangling=true")
	}
	raw, err := podmanList[storeImage](ctx, e, "images", append(args, "--format", "json")...)
	if err != nil {
		return nil, err
	}
	out := make([]StoreImage, 0, len(raw))
	for _, r := range raw {
		out = append(out, StoreImage{
			ID: bareID(r.ID), Names: r.Names, Size: r.Size, Labels: r.Labels,
			Containers: r.Containers, Created: time.Unix(r.Created, 0),
		})
	}
	return out, nil
}

// inspectedImage is the part of `podman image inspect` that names an image's layers.
type inspectedImage struct {
	ID     string
	RootFS struct{ Layers []string }
}

// ImageLayers is each image's RootFS layers, base first, by bare id, in one inspect. An image
// built on another starts with all of the other's layers, which is how a caller tells that
// podman would refuse to remove the other.
func (e Engine) ImageLayers(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	raw, err := podmanList[inspectedImage](ctx, e, "the layers of each image", append([]string{"image", inspect, "--format", "json"}, ids...)...)
	if err != nil {
		return nil, err
	}
	for _, r := range raw {
		out[bareID(r.ID)] = r.RootFS.Layers
	}
	return out, nil
}

// StoreContainers is every container, running or not, and those other tools made.
func (e Engine) StoreContainers(ctx context.Context) ([]StoreContainer, error) {
	raw, err := podmanList[storeContainer](ctx, e, "containers", "ps", "--all", "--external", "--format", "json")
	if err != nil {
		return nil, err
	}
	out := make([]StoreContainer, 0, len(raw))
	for _, r := range raw {
		c := StoreContainer{
			ID: r.ID, ImageID: bareID(r.ImageID), Created: r.Created.Time, State: r.State,
			External: r.State == "storage",
		}
		if len(r.Names) > 0 {
			c.Name = r.Names[0]
		}
		if len(r.Command) > 0 {
			c.Command = r.Command[0]
		}
		out = append(out, c)
	}
	return out, nil
}

// DanglingVolumes is the names of the volumes no container uses.
func (e Engine) DanglingVolumes(ctx context.Context) ([]string, error) {
	res, err := e.run(ctx, "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}")
	if err != nil {
		return nil, fmt.Errorf("list dangling volumes: %w", err)
	}
	return lines(res.Stdout), nil
}

// StoreVolume is one volume of `podman volume ls --format json`.
type StoreVolume struct {
	Name       string
	Mountpoint string
}

// StoreVolumes is every volume, with the directory holding its data.
func (e Engine) StoreVolumes(ctx context.Context) ([]StoreVolume, error) {
	return podmanList[StoreVolume](ctx, e, "volumes", "volume", "ls", "--format", "json")
}

// inspectedContainer is the part of `podman container inspect` that names its mounts.
type inspectedContainer struct {
	ID     string
	Mounts []struct {
		Type string
		Name string
	}
}

// ContainerVolumes is the names of the named volumes each container mounts, by container id.
// Only podman's own containers can be inspected, so ids is never an external container's.
func (e Engine) ContainerVolumes(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	raw, err := podmanList[inspectedContainer](ctx, e, "the mounts of each container", append([]string{"container", inspect}, ids...)...)
	if err != nil {
		return nil, err
	}
	for _, c := range raw {
		out[c.ID] = nil
		for _, m := range c.Mounts {
			if m.Type == "volume" && m.Name != "" {
				out[c.ID] = append(out[c.ID], m.Name)
			}
		}
	}
	return out, nil
}

// RemoveContainer is `rm ID`, never forced: the engine refuses a running container.
func (e Engine) RemoveContainer(ctx context.Context, id string) error {
	if _, err := e.run(ctx, "rm", id); err != nil {
		return fmt.Errorf("remove container %s: %w", id, err)
	}
	return nil
}

// RemoveVolume is `volume rm NAME`, never forced: the engine refuses a volume a container uses.
func (e Engine) RemoveVolume(ctx context.Context, name string) error {
	if _, err := e.run(ctx, "volume", "rm", name); err != nil {
		return fmt.Errorf("remove volume %s: %w", name, err)
	}
	return nil
}

// RemoveImage is `image rm REF`, never forced: the engine refuses an image a container uses or
// another image is built on, and a tag of an image with other names only untags it.
func (e Engine) RemoveImage(ctx context.Context, ref string) error {
	if _, err := e.run(ctx, "image", "rm", ref); err != nil {
		return fmt.Errorf("remove image %s: %w", ref, err)
	}
	return nil
}

// storeHasArgs is podman's exists question for each kind it answers for; a container another
// tool made counts.
var storeHasArgs = map[string][]string{
	KindImage:     {KindImage, exists},
	KindVolume:    {KindVolume, exists},
	KindContainer: {KindContainer, exists, "--external"},
	KindNetwork:   {KindNetwork, exists},
}

// dockerList is docker's list, and its name template, for each kind StoreHas answers for on
// docker, which has no exists question; the name filter matches a substring, so the answer is
// an exact line.
var dockerList = map[string][]string{
	KindContainer: {"{{.Names}}", "ps", "--all"},
	KindNetwork:   {"{{.Name}}", KindNetwork, "ls"},
}

// The kinds StoreHas answers for, as podman names its object verbs.
const (
	KindImage     = "image"
	KindVolume    = "volume"
	KindContainer = "container"
	KindNetwork   = "network"
	exists        = "exists"
	formatFlag    = "--format"
)

// StoreHas is podman's own `KIND exists KEY`: exit 0 is there, exit 1 is gone, and any other
// end is podman failing, so an error, never a gone. Docker is asked about a container or a
// network by its list filtered on the name; a list that fails is an error, never a gone.
func (e Engine) StoreHas(ctx context.Context, kind, key string) (bool, error) {
	if err := e.known(); err != nil {
		return false, err
	}
	if list, ok := dockerList[kind]; ok && e.kind == Docker {
		return e.dockerHas(ctx, kind, key, list)
	}
	if e.kind != Podman {
		return false, fmt.Errorf("whether there is a %s of %s: %w", kind, e.kind, ErrPodmanOnly)
	}
	args, ok := storeHasArgs[kind]
	if !ok {
		return false, fmt.Errorf("podman has no exists question for a %s", kind)
	}
	_, err := e.run(ctx, append(slices.Clone(args), key)...)
	if exit, isExit := errors.AsType[*execx.ExitError](err); isExit && exit != nil && exit.Code == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ask podman whether there is a %s %s: %w", kind, key, err)
	}
	return true, nil
}

// dockerHas is docker's list of the kind filtered on key, holding a line that is key exactly.
func (e Engine) dockerHas(ctx context.Context, kind, key string, list []string) (bool, error) {
	args := append(slices.Clone(list[1:]), "--filter", "name="+key, formatFlag, list[0])
	res, err := e.run(ctx, args...)
	if err != nil {
		return false, fmt.Errorf("ask docker whether there is a %s %s: %w", kind, key, err)
	}
	return slices.Contains(lines(res.Stdout), key), nil
}

// RemoveExternalContainer removes a container another tool made: podman removes one it did not
// create only when forced.
func (e Engine) RemoveExternalContainer(ctx context.Context, id string) error {
	if err := e.known(); err != nil {
		return err
	}
	if e.kind != Podman {
		return fmt.Errorf("external container of %s: %w", e.kind, ErrPodmanOnly)
	}
	if _, err := e.run(ctx, "rm", "--force", id); err != nil {
		return fmt.Errorf("remove container %s: %w", id, err)
	}
	return nil
}

// RepositoryTags is each tag of the repository with the id of the image it names.
func (e Engine) RepositoryTags(ctx context.Context, repository string) ([]Tag, error) {
	res, err := e.run(ctx, "images", "--no-trunc", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}", repository)
	if err != nil {
		return nil, fmt.Errorf("list the tags of %s: %w", repository, err)
	}
	var tags []Tag
	for _, line := range lines(res.Stdout) {
		name, imageID, ok := strings.Cut(line, " ")
		if !ok || strings.TrimSpace(imageID) == "" {
			return nil, fmt.Errorf("list the tags of %s: cannot read %q", repository, line)
		}
		if strings.HasSuffix(name, ":<none>") {
			continue
		}
		tags = append(tags, Tag{Name: name, ID: bareID(imageID)})
	}
	return tags, nil
}

// ContainerImageIDs is the image id of every container, running or not.
func (e Engine) ContainerImageIDs(ctx context.Context) ([]string, error) {
	res, err := e.run(ctx, "ps", "--all", "--quiet", "--no-trunc")
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	ids := lines(res.Stdout)
	if len(ids) == 0 {
		return nil, nil
	}
	res, err = e.run(ctx, append([]string{"container", inspect, "--format", "{{.Image}}"}, ids...)...)
	if err != nil {
		return nil, fmt.Errorf("ask which image each container uses: %w", err)
	}
	var images []string
	for _, line := range lines(res.Stdout) {
		images = append(images, bareID(line))
	}
	return images, nil
}
