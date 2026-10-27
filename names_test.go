package agentsmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

// TestChainNamesMatchExactly: a directory's file is the first of Names
// that equals an entry's name byte for byte. fstest.MapFS is exact on
// every host, so this table gives the case-sensitive answer whatever
// the file system the tests run on, which is the answer Chain now
// gives on every host.
func TestChainNamesMatchExactly(t *testing.T) {
	fsys := fstest.MapFS{
		"agents.md":              {Data: []byte("lower\n")},
		"sub/Agents.MD":          {Data: []byte("mixed\n")},
		"sub/deep/AGENTS.md":     {Data: []byte("exact\n")},
		"dir/AGENTS.md/note.txt": {Data: []byte("a directory named AGENTS.md\n")},
		"dir/CLAUDE.md":          {Data: []byte("claude\n")},
	}
	tests := []struct {
		name    string
		path    string
		names   []string
		want    []string
		omitted []Omitted
		err     string
	}{
		{name: "case-only variants are not AGENTS.md", path: "sub/deep", want: []string{"sub/deep/AGENTS.md"}},
		{name: "a lowercase name finds the lowercase file", path: "sub/deep", names: []string{"agents.md"}, want: []string{"agents.md"}},
		{name: "a mixed-case name finds that spelling alone", path: "sub/deep", names: []string{"Agents.MD", "agents.md"}, want: []string{"agents.md", "sub/Agents.MD"}},
		{name: "a directory of that name is not a file", path: "dir", names: []string{"AGENTS.md", "CLAUDE.md"}, want: []string{"dir/CLAUDE.md"}},
		{name: "a name with a separator", path: "sub", names: []string{"deep/AGENTS.md"}, err: "not a valid name"},
		{name: "an empty name", path: "sub", names: []string{""}, err: "not a valid name"},
		{name: "dot-dot", path: "sub", names: []string{".."}, err: "not a valid name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Chain(tt.path, Options{FS: fsys, Names: tt.names})
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Chain() error = %v, want containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := paths(res.Files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Chain().Files = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(res.Omitted, tt.omitted) {
				t.Errorf("Chain().Omitted = %+v, want %+v", res.Omitted, tt.omitted)
			}
		})
	}
}

// TestChainOSNamesMatchExactly: on the OS too, agents.md is not
// AGENTS.md. On a case-insensitive file system the stat that used to
// decide answered for the other spelling; the listing does not, so the
// first two cases hold on every host. The third needs both spellings
// side by side, which only a case-sensitive file system can hold.
func TestChainOSNamesMatchExactly(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	chain := func(names ...string) Result {
		t.Helper()
		res, err := Chain(dir, Options{Root: dir, Names: names})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	write("agents.md", "lower\n")
	t.Run("lowercase file, default name", func(t *testing.T) {
		if res := chain(); len(res.Files) != 0 || len(res.Omitted) != 0 {
			t.Errorf("Chain() = %+v, want nothing for agents.md", res)
		}
	})
	t.Run("lowercase file, lowercase name", func(t *testing.T) {
		res := chain("agents.md")
		if want := []File{{Path: filepath.Join(dir, "agents.md"), Content: "lower\n"}}; !reflect.DeepEqual(res.Files, want) {
			t.Errorf("Chain().Files = %+v, want %+v", res.Files, want)
		}
	})
	t.Run("both spellings", func(t *testing.T) {
		write("AGENTS.md", "upper\n")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Skip("the file system folds case: one file holds both spellings")
		}
		upper, lower := filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "agents.md")
		if res := chain(); !reflect.DeepEqual(res.Files, []File{{Path: upper, Content: "upper\n"}}) || len(res.Omitted) != 0 {
			t.Errorf("Chain() = %+v, want AGENTS.md alone", res)
		}
		res := chain("agents.md", "AGENTS.md")
		if !reflect.DeepEqual(res.Files, []File{{Path: lower, Content: "lower\n"}}) {
			t.Errorf("Chain().Files = %+v, want agents.md", res.Files)
		}
		if want := []Omitted{{Path: upper, Size: 6, Reason: Shadowed, By: lower}}; !reflect.DeepEqual(res.Omitted, want) {
			t.Errorf("Chain().Omitted = %+v, want %+v", res.Omitted, want)
		}
	})
}

// TestChainUnlistableDirectory: a directory that can be searched but
// not listed may hold a file the chain would include, so it is an
// error rather than a directory with nothing in it.
func TestChainUnlistableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no search-only directories")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "locked")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o311); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("the directory is still listable, as it is for root")
	}
	res, err := Chain(dir, Options{Root: root})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Chain() = %+v, %v; want a permission error", res, err)
	}
}
