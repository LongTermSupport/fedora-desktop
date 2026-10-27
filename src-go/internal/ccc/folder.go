package ccc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

// FolderName is the folder under the home directory every session runs in. Not named after
// one agent: the folder is shared by every agent ccc can run.
const FolderName = "CCC"

// firstBranch is the repository's branch; it never has a remote.
const firstBranch = "main"

// ignoreFile keeps out of the snapshots what is rebuilt, cached, operating-system junk or
// looks like a secret.
const ignoreFile = `# Kept out of ccc's snapshots: rebuilt, cached, junk, or secret-looking.
node_modules/
.venv/
venv/
__pycache__/
*.pyc
.cache/
.pytest_cache/
.mypy_cache/
.ruff_cache/
dist/
build/
target/
out/
*.log
*.tmp
*.swp
*~
.DS_Store
Thumbs.db
desktop.ini
.env
.env.*
*.key
*.pem
*.p12
*.pfx
*secret*
*credentials*
id_rsa*
id_ed25519*
.netrc
`

// guideText is the guide file a new folder starts with; the person may change it.
const guideText = `# This folder

This is a general-purpose folder for chatting, writing, planning and small projects. The
person using it may not write code: explain in plain words, ask before anything drastic, and
keep files tidy and clearly named.

Every session is saved as a snapshot in a local git history when it ends. There is no remote
and nothing is ever pushed.
`

// Identity is the account the snapshots are committed as, in the folder's own git config.
type Identity struct {
	Login    string
	FullName string
}

func (i Identity) name() string {
	if i.FullName != "" {
		return i.FullName
	}
	return i.Login
}

func (i Identity) email() string { return i.Login + "@localhost" }

// prepareFolder makes dir the agent's folder: created private when missing, refused when it is
// a symbolic link or not a folder, and made a repository with its own identity, an ignore file,
// a guide file and a first commit when it is not one yet. Files already there are kept.
func prepareFolder(ctx context.Context, git *gitx.Git, dir string, agent Agent, who Identity) error {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if merr := os.Mkdir(dir, 0o700); merr != nil {
			return fmt.Errorf("make %s: %w", dir, merr)
		}
	case err != nil:
		return fmt.Errorf("look at %s: %w", dir, err)
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a symbolic link; ccc runs only in a real folder there", dir)
	case !info.IsDir():
		return fmt.Errorf("%s is not a folder; move it aside and run ccc again", dir)
	}
	present, err := exists(filepath.Join(dir, ".git"))
	if err != nil || present {
		return err
	}
	if err := git.InitRepo(ctx, dir, firstBranch); err != nil {
		return err
	}
	if err := git.SetLocalConfig(ctx, dir, "user.name", who.name()); err != nil {
		return err
	}
	if err := git.SetLocalConfig(ctx, dir, "user.email", who.email()); err != nil {
		return err
	}
	for name, text := range map[string]string{".gitignore": ignoreFile, agent.GuideFile: guideText} {
		if err := writeIfMissing(filepath.Join(dir, name), text); err != nil {
			return err
		}
	}
	return git.CommitAll(ctx, dir, "ccc: start the folder")
}

func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("look at %s: %w", path, err)
	}
}

func writeIfMissing(path, text string) error {
	present, err := exists(path)
	if err != nil || present {
		return err
	}
	return fsx.WriteAtomic(path, filepath.Base(path), []byte(text), 0o600)
}
