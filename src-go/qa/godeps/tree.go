package godeps

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"slices"
)

// compareTrees names every file that differs between want (what go mod vendor writes) and got
// (the committed vendor directory), sorted by path, and counts the files of want (G9). An
// absent tree is an empty one.
func compareTrees(want, got string) ([]string, int, error) {
	wantFiles, err := readTree(want)
	if err != nil {
		return nil, 0, err
	}
	gotFiles, err := readTree(got)
	if err != nil {
		return nil, 0, err
	}
	var diffs []string
	for rel, body := range wantFiles {
		other, present := gotFiles[rel]
		switch {
		case !present:
			diffs = append(diffs, rel+" is missing")
		case !bytes.Equal(body, other):
			diffs = append(diffs, rel+" differs")
		}
	}
	for rel := range gotFiles {
		if _, present := wantFiles[rel]; !present {
			diffs = append(diffs, rel+" is not what go mod vendor writes")
		}
	}
	slices.Sort(diffs)
	return diffs, len(wantFiles), nil
}

// readTree is every file under root by its slash-separated path below it.
func readTree(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return files, nil
	}
	tree := os.DirFS(root)
	err := fs.WalkDir(tree, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		body, err := fs.ReadFile(tree, path)
		if err != nil {
			return err
		}
		files[path] = body
		return nil
	})
	return files, err
}
