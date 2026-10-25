// Package safepath verifies filesystem namespaces before they reach Git.
// Root-owned system ancestors and the effective UID are trusted; managed leaves
// must belong to the effective UID. Shared writable ancestors are never trusted.
package safepath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type Snapshot struct {
	path       string
	identities []os.FileInfo
}

func Trusted(info os.FileInfo, systemAncestor bool) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (stat.Uid != uint32(os.Geteuid()) && (!systemAncestor || stat.Uid != 0)) || info.Mode().Perm()&0o022 != 0 {
		return errors.New("filesystem path must have trusted ownership and no group/other write permission")
	}
	return nil
}

// Open walks one component at a time through stable directory descriptors.
// Missing components may be created privately, without chmodding existing trees.
func Open(path string, create bool) (*os.Root, error) {
	root, _, err := walk(path, create, false)
	return root, err
}

// Capture records existing directory identities, including the nearest parent of
// a missing endpoint. A later Check rejects new components as well as replaced existing components.
func Capture(path string) (*Snapshot, error) {
	root, identities, err := walk(path, false, true)
	if root != nil {
		_ = root.Close()
	}
	if err != nil {
		return nil, err
	}
	return &Snapshot{path: path, identities: identities}, nil
}

func (s *Snapshot) Check() error {
	current, err := Capture(s.path)
	if err != nil {
		return err
	}
	if len(current.identities) != len(s.identities) {
		return errors.New("filesystem path identity changed")
	}
	for i, info := range s.identities {
		if !os.SameFile(info, current.identities[i]) {
			return errors.New("filesystem path identity changed")
		}
	}
	return nil
}

func walk(path string, create, missingOK bool) (*os.Root, []os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, errors.New("trusted path must be absolute and clean")
	}
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*os.Root, []os.FileInfo, error) { _ = root.Close(); return nil, nil, err }
	var identities []os.FileInfo
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	for i := 0; ; i++ {
		file, err := root.Open(".")
		if err != nil {
			return fail(err)
		}
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return fail(err)
		}
		if err := Trusted(info, i < len(components)); err != nil {
			return fail(err)
		}
		identities = append(identities, info)
		if i == len(components) {
			return root, identities, nil
		}
		name := components[i]
		info, err = root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return fail(err)
			}
			info, err = root.Lstat(name)
		}
		if errors.Is(err, os.ErrNotExist) && missingOK {
			return root, identities, nil
		}
		if err != nil {
			return fail(err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fail(fmt.Errorf("trusted path component must be a real directory: %s", name))
		}
		child, err := root.OpenRoot(name)
		if err != nil {
			return fail(err)
		}
		childFile, err := child.Open(".")
		if err != nil {
			_ = child.Close()
			return fail(err)
		}
		opened, err := childFile.Stat()
		childFile.Close()
		if err != nil || !os.SameFile(info, opened) {
			_ = child.Close()
			return fail(errors.New("filesystem path changed during validation"))
		}
		_ = root.Close()
		root = child
	}
}
