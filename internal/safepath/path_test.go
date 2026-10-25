package safepath

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestNamespacePolicy(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "repo")
	root, err := Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	snapshot, err := Capture(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Check(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if snapshot.Check() == nil {
		t.Fatal("accepted replacement")
	}
	for _, mode := range []os.FileMode{0o777, 0o770, 0o722} {
		if err := os.Chmod(parent, mode); err != nil {
			t.Fatal(err)
		}
		if root, err := Open(filepath.Join(parent, "missing"), true); err == nil {
			root.Close()
			t.Fatal("accepted writable ancestor")
		}
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(parent, "missing")
	snapshot, err = Capture(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(missing, 0o700); err != nil {
		t.Fatal(err)
	}
	if snapshot.Check() == nil {
		t.Fatal("accepted endpoint appearing after load")
	}
	if err := os.Symlink(path, filepath.Join(parent, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(filepath.Join(parent, "alias")); err == nil {
		t.Fatal("accepted symlink")
	}
}

type ownerInfo struct {
	os.FileInfo
	stat syscall.Stat_t
}

func (i ownerInfo) Sys() any { return &i.stat }

func TestOwnershipPolicy(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign := ownerInfo{FileInfo: info, stat: syscall.Stat_t{Uid: uint32(os.Geteuid() + 1)}}
	if Trusted(foreign, true) == nil {
		t.Fatal("accepted foreign owner")
	}
	root := ownerInfo{FileInfo: info, stat: syscall.Stat_t{Uid: 0}}
	if err := Trusted(root, true); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && Trusted(root, false) == nil {
		t.Fatal("accepted root-owned managed leaf")
	}
}
