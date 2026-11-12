package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/zekihan/git-mirrorer/internal/cachefs"
)

// RefMapping relocates a complete namespace for one destination.
type RefMapping struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

func ValidateRefMappings(mappings []RefMapping) error {
	valid := func(prefix string) bool {
		if !strings.HasPrefix(prefix, "refs/") || !strings.HasSuffix(prefix, "/") || prefix == "refs/" {
			return false
		}
		if strings.ContainsAny(prefix, " ~^:?*[\\") || strings.Contains(prefix, "..") || strings.Contains(prefix, "@{") || strings.ContainsFunc(prefix, unicode.IsControl) {
			return false
		}
		for _, part := range strings.Split(strings.TrimSuffix(prefix, "/"), "/") {
			if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
				return false
			}
		}
		return true
	}
	overlaps := func(a, b string) bool { return strings.HasPrefix(a, b) || strings.HasPrefix(b, a) }
	for i, m := range mappings {
		if !valid(m.From) || !valid(m.To) {
			return errors.New("refMappings require complete refs/ namespace prefixes ending in /")
		}
		for j, other := range mappings {
			if overlaps(m.From, other.To) || (i != j && (overlaps(m.From, other.From) || overlaps(m.To, other.To))) {
				return errors.New("refMappings namespaces must not overlap or form chains")
			}
		}
	}
	return nil
}

const mappedRefLimit = 100_000
const mappedRefBytes = 32 * 1024 * 1024

type refBuffer struct{ bytes.Buffer }

func (b *refBuffer) Write(p []byte) (int, error) {
	if len(p) > mappedRefBytes-b.Len() {
		return 0, errors.New("mapped ref inventory exceeds 32 MiB")
	}
	return b.Buffer.Write(p)
}

func (c *Client) pushMapped(ctx context.Context, path, destination string, atomic bool, mappings []RefMapping) (changes Changes, err error) {
	if err := ValidateRefMappings(mappings); err != nil {
		return Changes{}, err
	}
	var refs refBuffer
	if err := c.command(ctx, path, nil, &refs, "for-each-ref", fmt.Sprintf("--count=%d", mappedRefLimit+1), "--format=%(objectname) %(refname)", "refs/"); err != nil {
		return Changes{}, err
	}
	lines := strings.Split(strings.TrimSuffix(refs.String(), "\n"), "\n")
	if refs.Len() == 0 {
		lines = nil
	}
	if len(lines) > mappedRefLimit {
		return Changes{}, errors.New("mapped ref inventory exceeds 100000 refs")
	}
	seen := make(map[string]bool, len(lines))
	for i, line := range lines {
		oid, ref, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(ref, "refs/") {
			return Changes{}, errors.New("invalid ref inventory")
		}
		for _, m := range mappings {
			if strings.HasPrefix(ref, m.From) {
				ref = m.To + strings.TrimPrefix(ref, m.From)
				break
			}
		}
		if seen[ref] {
			return Changes{}, errors.New("refMappings collide with another source ref")
		}
		seen[ref] = true
		lines[i] = oid + " " + ref
	}
	// Packed refs must be sorted by name, not by object ID. This view borrows
	// objects from the locked cache but never changes its refs or configuration.
	sort.Slice(lines, func(i, j int) bool { return strings.SplitN(lines[i], " ", 2)[1] < strings.SplitN(lines[j], " ", 2)[1] })
	for ref := range seen {
		for parent := ref; strings.Contains(parent, "/"); {
			parent = parent[:strings.LastIndexByte(parent, '/')]
			if seen[parent] {
				return Changes{}, errors.New("refMappings collide with another source namespace")
			}
		}
	}
	var format bytes.Buffer
	if err := c.command(ctx, path, nil, &format, "rev-parse", "--show-object-format"); err != nil {
		return Changes{}, err
	}
	objectFormat := strings.TrimSpace(format.String())
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return Changes{}, errors.New("unsupported object format")
	}
	objectPath, err := filepath.Abs(filepath.Join(path, "objects"))
	if err != nil || strings.ContainsAny(objectPath, "\r\n") {
		return Changes{}, errors.New("invalid cache object path")
	}
	scratch, err := os.MkdirTemp(filepath.Dir(path), ".mapped-refs-*")
	if err != nil {
		return Changes{}, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err = errors.Join(err, cachefs.RemoveTree(cleanupCtx, scratch, 1000))
	}()
	if err = c.command(ctx, "", nil, nil, "-c", "init.defaultRefFormat=files", "init", "--bare", "--template=", "--object-format="+objectFormat, scratch); err != nil {
		return Changes{}, err
	}
	if err = os.MkdirAll(filepath.Join(scratch, "objects", "info"), 0o700); err != nil {
		return Changes{}, err
	}
	if err = os.WriteFile(filepath.Join(scratch, "objects", "info", "alternates"), []byte(objectPath+"\n"), 0o600); err != nil {
		return Changes{}, err
	}
	if len(lines) > 0 {
		if err = os.WriteFile(filepath.Join(scratch, "packed-refs"), []byte("# pack-refs with: sorted\n"+strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			return Changes{}, err
		}
	}
	var actual refBuffer
	if err = c.command(ctx, scratch, nil, &actual, "for-each-ref", fmt.Sprintf("--count=%d", mappedRefLimit+1), "--sort=refname", "--format=%(objectname) %(refname)", "refs/"); err != nil {
		return Changes{}, err
	}
	expected := strings.Join(lines, "\n")
	if len(lines) > 0 {
		expected += "\n"
	}
	if actual.String() != expected {
		return Changes{}, errors.New("mapped ref view does not match source inventory")
	}
	return c.push(ctx, scratch, destination, atomic)
}
