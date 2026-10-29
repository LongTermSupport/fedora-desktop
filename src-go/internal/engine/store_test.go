package engine

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// imagesJSON is the shape `podman images --format json` prints (podman 4 and 5): a list whose
// Id has no "sha256:" prefix, Created in Unix seconds, and a dangling image with no Names.
const imagesJSON = `[
 {"Id":"1111111111111111111111111111111111111111111111111111111111111111","ParentId":"","RepoTags":null,
  "Names":["localhost/claude-yolo:latest"],"Size":3221225472,"SharedSize":0,"VirtualSize":3221225472,
  "Labels":{"claude-yolo-version":"9.9.9"},"Containers":2,"Created":1760000000,"CreatedAt":"2025-10-09T08:53:20Z"},
 {"Id":"sha256:2222222222222222222222222222222222222222222222222222222222222222","Size":104857600,
  "Labels":null,"Containers":0,"Created":1750000000,"Dangling":true}
]
`

// psJSON is the shape `podman ps -a --external --format json` prints: podman 4 and 5 write
// Created as an RFC 3339 time; a container another tool made (buildah) is in the "storage"
// state.
const psJSON = `[
 {"Id":"c1","Names":["project-a"],"Image":"localhost/claude-yolo:latest",
  "ImageID":"1111111111111111111111111111111111111111111111111111111111111111",
  "Created":"2026-10-27T10:00:00.123456789Z","State":"running","Command":["claude"]},
 {"Id":"c2","Names":["happy_build"],"Image":"",
  "ImageID":"2222222222222222222222222222222222222222222222222222222222222222",
  "Created":"2026-09-01T00:00:00Z","State":"storage","Command":["buildah"]}
]
`

func TestStoreImagesReadsPodmansList(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "images", "--format", "json"}, execx.Result{Stdout: []byte(imagesJSON)})
	got, err := e.StoreImages(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d images, want 2", len(got))
	}
	latest, none := got[0], got[1]
	if latest.ID != "1111111111111111111111111111111111111111111111111111111111111111" ||
		!slices.Equal(latest.Names, []string{"localhost/claude-yolo:latest"}) ||
		latest.Size != 3221225472 || latest.Containers != 2 ||
		latest.Labels["claude-yolo-version"] != "9.9.9" || !latest.Created.Equal(time.Unix(1760000000, 0)) {
		t.Errorf("first image read as %+v", latest)
	}
	if none.ID != "2222222222222222222222222222222222222222222222222222222222222222" || len(none.Names) != 0 {
		t.Errorf("the sha256: prefix is not dropped or the names are wrong: %+v", none)
	}
}

func TestStoreImagesAsksForTheDanglingOnes(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "images", "--filter", "dangling=true", "--format", "json"}, execx.Result{Stdout: []byte("[]\n")})
	got, err := e.StoreImages(context.Background(), true)
	if err != nil || len(got) != 0 {
		t.Fatalf("StoreImages(dangling) = %v, %v", got, err)
	}
}

func TestStoreContainersReadsEveryContainerTheExternalOnesToo(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "ps", "--all", "--external", "--format", "json"}, execx.Result{Stdout: []byte(psJSON)})
	got, err := e.StoreContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d containers, want 2", len(got))
	}
	ccy, build := got[0], got[1]
	if ccy.ID != "c1" || ccy.Name != "project-a" || ccy.External ||
		ccy.ImageID != "1111111111111111111111111111111111111111111111111111111111111111" ||
		!ccy.Created.Equal(time.Date(2026, 10, 27, 10, 0, 0, 123456789, time.UTC)) {
		t.Errorf("the ccy container read as %+v", ccy)
	}
	if !build.External || build.Name != "happy_build" || build.Command != "buildah" {
		t.Errorf("the buildah container read as %+v", build)
	}
}

// An older podman wrote a container's Created as Unix seconds; that is read too.
func TestStoreContainersReadsCreatedInSeconds(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "ps"}, execx.Result{Stdout: []byte(`[{"Id":"c3","Created":1760000000,"State":"exited"}]`)})
	got, err := e.StoreContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Created.Equal(time.Unix(1760000000, 0)) || got[0].Name != "" {
		t.Fatalf("read %+v", got)
	}
}

func TestStoreListsRefuseWhatIsNotAList(t *testing.T) {
	cases := []struct {
		name string
		out  string
	}{
		{"not json", "Error: something\n"},
		{"a created time that is neither", `[{"Id":"c","Created":true}]`},
		{"a created string that is not a time", `[{"Id":"c","Created":"yesterday"}]`},
	}
	for _, c := range cases {
		e, f := engineOn(t, Podman)
		f.On([]string{"podman"}, execx.Result{Stdout: []byte(c.out)})
		if _, err := e.StoreContainers(context.Background()); err == nil {
			t.Errorf("%s: StoreContainers read %q", c.name, c.out)
		}
		if _, err := e.StoreImages(context.Background(), false); err == nil && c.name == "not json" {
			t.Errorf("%s: StoreImages read %q", c.name, c.out)
		}
	}
}

func TestStoreQuestionsFailWhenTheEngineFails(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: database is locked\n")})
	ctx := context.Background()
	errOf := func(_ any, err error) error { return err }
	checks := map[string]error{
		"StoreImages":       errOf(e.StoreImages(ctx, true)),
		"StoreContainers":   errOf(e.StoreContainers(ctx)),
		"DanglingVolumes":   errOf(e.DanglingVolumes(ctx)),
		"RemoveImage":       e.RemoveImage(ctx, "x"),
		"RemoveExternal":    e.RemoveExternalContainer(ctx, "x"),
		"RepositoryTags":    errOf(e.RepositoryTags(ctx, "claude-yolo")),
		"ContainerImageIDs": errOf(e.ContainerImageIDs(ctx)),
	}
	for name, err := range checks {
		if exit, ok := errors.AsType[*execx.ExitError](err); !ok || exit.Code != 125 {
			t.Errorf("%s = %v, want the engine's exit 125", name, err)
		}
	}
}

// The store's own lists (images and containers as JSON, the external containers) are podman's;
// docker is refused before anything runs.
func TestStoreListsArePodmansOnly(t *testing.T) {
	e, f := engineOn(t, Docker)
	ctx := context.Background()
	errOf := func(_ any, err error) error { return err }
	for name, err := range map[string]error{
		"StoreImages":     errOf(e.StoreImages(ctx, true)),
		"StoreContainers": errOf(e.StoreContainers(ctx)),
		"RemoveExternal":  e.RemoveExternalContainer(ctx, "x"),
	} {
		if !errors.Is(err, ErrPodmanOnly) {
			t.Errorf("%s on docker = %v, want ErrPodmanOnly", name, err)
		}
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("docker was run: %v", f.Calls())
	}
	var zero Engine
	if _, err := zero.StoreImages(ctx, true); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("the zero Engine = %v", err)
	}
}

// imageInspectJSON is the part of `podman image inspect --format json` ImageLayers reads: each
// image's id (podman writes it bare, docker with "sha256:") and its RootFS layers, base first.
const imageInspectJSON = `[
 {"Id":"1111111111111111111111111111111111111111111111111111111111111111","Digest":"sha256:d",
  "RepoTags":["localhost/claude-yolo:latest"],"RootFS":{"Type":"layers","Layers":["sha256:l1","sha256:l2"]}},
 {"Id":"sha256:2222222222222222222222222222222222222222222222222222222222222222",
  "RootFS":{"Type":"layers","Layers":["sha256:l1"]}}
]
`

func TestImageLayersAreEachImagesRootFSLayersInOneInspect(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "image", "inspect", "--format", "json", "id-1", "id-2"}, execx.Result{Stdout: []byte(imageInspectJSON)})
	got, err := e.ImageLayers(context.Background(), []string{"id-1", "id-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !slices.Equal(got["1111111111111111111111111111111111111111111111111111111111111111"], []string{"sha256:l1", "sha256:l2"}) ||
		!slices.Equal(got["2222222222222222222222222222222222222222222222222222222222222222"], []string{"sha256:l1"}) {
		t.Fatalf("ImageLayers = %v", got)
	}
	none, err := e.ImageLayers(context.Background(), nil)
	if err != nil || len(none) != 0 || len(f.Calls()) != 1 {
		t.Fatalf("no ids: %v, %v, ran %v", none, err, f.Calls())
	}
	d, df := engineOn(t, Docker)
	if _, err := d.ImageLayers(context.Background(), []string{"x"}); !errors.Is(err, ErrPodmanOnly) || len(df.Calls()) != 0 {
		t.Fatalf("docker: %v, ran %v", err, df.Calls())
	}
	f.On([]string{"podman", "image", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
	if _, err := e.ImageLayers(context.Background(), []string{"x"}); err == nil {
		t.Fatal("a failed inspect was read")
	}
}

func TestDanglingVolumesAreTheirNames(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"},
		execx.Result{Stdout: []byte("vol-a\n\nvol-b\n")})
	got, err := e.DanglingVolumes(context.Background())
	if err != nil || !slices.Equal(got, []string{"vol-a", "vol-b"}) {
		t.Fatalf("DanglingVolumes = %v, %v", got, err)
	}
}

func TestRemovalsAreTheEnginesOwnUnforcedImageRemoveAndAForcedExternalOne(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman"}, execx.Result{})
	ctx := context.Background()
	if err := e.RemoveImage(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveExternalContainer(ctx, "c2"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Line() != "podman image rm abc" || calls[1].Line() != "podman rm --force c2" {
		t.Fatalf("ran %v", calls)
	}
}

// RepositoryTags and ContainerImageIDs use only what podman and docker both print, so ccy's
// self-prune works under either engine.
func TestRepositoryTagsAreEachTagWithItsID(t *testing.T) {
	for _, kind := range []Kind{Podman, Docker} {
		e, f := engineOn(t, kind)
		f.On([]string{string(kind), "images", "--no-trunc", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}", "claude-yolo"},
			execx.Result{Stdout: []byte("localhost/claude-yolo:latest sha256:aaa\nlocalhost/claude-yolo:base sha256:bbb\n<none>:<none> sha256:ccc\n")})
		got, err := e.RepositoryTags(context.Background(), "claude-yolo")
		if err != nil {
			t.Fatal(err)
		}
		want := []Tag{{Name: "localhost/claude-yolo:latest", ID: "aaa"}, {Name: "localhost/claude-yolo:base", ID: "bbb"}}
		if !slices.Equal(got, want) {
			t.Errorf("%s: RepositoryTags = %v, want %v", kind, got, want)
		}
	}
}

func TestRepositoryTagsRefusesALineItCannotRead(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "images"}, execx.Result{Stdout: []byte("one-field-only\n")})
	if got, err := e.RepositoryTags(context.Background(), "claude-yolo"); err == nil {
		t.Fatalf("read %v", got)
	}
}

func TestContainerImageIDsInspectsEveryContainer(t *testing.T) {
	e, f := engineOn(t, Docker)
	f.On([]string{"docker", "ps", "--all", "--quiet", "--no-trunc"}, execx.Result{Stdout: []byte("c1\nc2\n")})
	f.On([]string{"docker", "container", "inspect", "--format", "{{.Image}}", "c1", "c2"},
		execx.Result{Stdout: []byte("sha256:aaa\nsha256:aaa\n")})
	got, err := e.ContainerImageIDs(context.Background())
	if err != nil || !slices.Equal(got, []string{"aaa", "aaa"}) {
		t.Fatalf("ContainerImageIDs = %v, %v", got, err)
	}
}

func TestContainerImageIDsOfNoContainerAsksNothingMore(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "ps"}, execx.Result{})
	got, err := e.ContainerImageIDs(context.Background())
	if err != nil || len(got) != 0 || len(f.Calls()) != 1 {
		t.Fatalf("ContainerImageIDs = %v, %v after %v", got, err, f.Calls())
	}
}

// An inspect that fails is an error, never "no image in use": a removal must not follow it.
func TestContainerImageIDsFailsWhenTheInspectFails(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "ps"}, execx.Result{Stdout: []byte("c1\n")})
	f.On([]string{"podman", "container", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: no such container c1\n")})
	if got, err := e.ContainerImageIDs(context.Background()); err == nil {
		t.Fatalf("read %v", got)
	}
}

// volumesJSON is the shape `podman volume ls --format json` prints.
const volumesJSON = `[
 {"Name":"db-data","Driver":"local","Mountpoint":"/home/u/.local/share/containers/storage/volumes/db-data/_data",
  "CreatedAt":"2025-01-01T00:00:00Z","Labels":{},"Scope":"local","Options":{},"MountCount":0},
 {"Name":"cache","Driver":"local","Mountpoint":"/home/u/.local/share/containers/storage/volumes/cache/_data"}
]
`

func TestStoreVolumesReadsPodmansList(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "volume", "ls", "--format", "json"}, execx.Result{Stdout: []byte(volumesJSON)})
	got, err := e.StoreVolumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []StoreVolume{
		{Name: "db-data", Mountpoint: "/home/u/.local/share/containers/storage/volumes/db-data/_data"},
		{Name: "cache", Mountpoint: "/home/u/.local/share/containers/storage/volumes/cache/_data"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("StoreVolumes = %v, want %v", got, want)
	}
}

// inspectJSON is the shape `podman container inspect` prints: each container's Mounts, of
// which only the named volumes are a volume's users.
const inspectJSON = `[
 {"Id":"c1","Name":"project-a","Mounts":[
   {"Type":"volume","Name":"db-data","Source":"/x/_data","Destination":"/var/lib/db"},
   {"Type":"bind","Source":"/home/u/p","Destination":"/workspace"}]},
 {"Id":"c2","Name":"other","Mounts":[]}
]
`

func TestContainerVolumesAreTheNamedVolumesEachMounts(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman", "container", "inspect", "c1", "c2"}, execx.Result{Stdout: []byte(inspectJSON)})
	got, err := e.ContainerVolumes(context.Background(), []string{"c1", "c2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !slices.Equal(got["c1"], []string{"db-data"}) || len(got["c2"]) != 0 {
		t.Fatalf("ContainerVolumes = %v", got)
	}
}

func TestContainerVolumesOfNoContainerAsksNothing(t *testing.T) {
	e, f := engineOn(t, Podman)
	got, err := e.ContainerVolumes(context.Background(), nil)
	if err != nil || len(got) != 0 || len(f.Calls()) != 0 {
		t.Fatalf("ContainerVolumes(nil) = %v, %v after %v", got, err, f.Calls())
	}
}

func TestVolumeQuestionsFailWhenTheEngineFails(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: boom\n")})
	ctx := context.Background()
	errOf := func(_ any, err error) error { return err }
	for name, err := range map[string]error{
		"StoreVolumes":     errOf(e.StoreVolumes(ctx)),
		"ContainerVolumes": errOf(e.ContainerVolumes(ctx, []string{"c1"})),
		"RemoveContainer":  e.RemoveContainer(ctx, "c1"),
		"RemoveVolume":     e.RemoveVolume(ctx, "v"),
	} {
		if exit, ok := errors.AsType[*execx.ExitError](err); !ok || exit.Code != 125 {
			t.Errorf("%s = %v, want the engine's exit 125", name, err)
		}
	}
}

// A container and a volume are removed by the engine's own unforced rm: it refuses a running
// container and a volume a container uses.
func TestContainerAndVolumeRemovalsAreUnforced(t *testing.T) {
	e, f := engineOn(t, Podman)
	f.On([]string{"podman"}, execx.Result{})
	ctx := context.Background()
	if err := e.RemoveContainer(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveVolume(ctx, "v"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Line() != "podman rm c1" || calls[1].Line() != "podman volume rm v" {
		t.Fatalf("ran %v", calls)
	}
}

func TestVolumeListsArePodmansOnly(t *testing.T) {
	e, f := engineOn(t, Docker)
	ctx := context.Background()
	errOf := func(_ any, err error) error { return err }
	for name, err := range map[string]error{
		"StoreVolumes":     errOf(e.StoreVolumes(ctx)),
		"ContainerVolumes": errOf(e.ContainerVolumes(ctx, []string{"c1"})),
	} {
		if !errors.Is(err, ErrPodmanOnly) {
			t.Errorf("%s on docker = %v, want ErrPodmanOnly", name, err)
		}
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("docker was run: %v", f.Calls())
	}
}
