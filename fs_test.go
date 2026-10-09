package agentsmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// treeFS returns the fixture tree as an fstest.MapFS, read from disk,
// so the same files can be walked through an FS that is not the OS.
func treeFS(t *testing.T, root string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		m[filepath.ToSlash(rel)] = &fstest.MapFile{Data: data, Mode: 0o644}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestChainFSMatchesOS: the fixture tree walked through os.DirFS and
// through an fstest.MapFS gives the same files, contents, omissions
// and render as the OS walk, with every path the file's name in the
// FS rather than its absolute path.
func TestChainFSMatchesOS(t *testing.T) {
	root := fixtureRoot(t)
	abs := func(name string) string { return filepath.Join(root, filepath.FromSlash(name)) }
	name := func(p string) string {
		if p == "" {
			return ""
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(rel)
	}
	size := func(n string) int64 {
		info, err := os.Stat(abs(n))
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	names := []string{"AGENTS.md", "CLAUDE.md"}
	override := []string{"AGENTS.override.md", "AGENTS.md"}
	tests := []struct {
		name string
		path string // a name in the tree
		opts Options
	}{
		{name: "file, default names", path: "sub/deep/file.txt"},
		{name: "directory", path: "sub/deep"},
		{name: "file about to be created", path: "sub/deep/new.go"},
		{name: "two names, nearest last", path: "sub/deep/file.txt", opts: Options{Names: names}},
		{name: "override shadows per directory", path: "shadow/inner", opts: Options{Names: override}},
		{name: "shadowed file over MaxBytes is not an error", path: "shadow/inner", opts: Options{Names: override, MaxBytes: 72}},
		{name: "names are a preference order", path: "shadow/inner", opts: Options{Names: []string{"AGENTS.md", "AGENTS.override.md"}}},
		{name: "root only", path: "other"},
		{name: "the root itself", path: "."},
		{name: "one byte short drops the nearest", path: "sub/deep/file.txt", opts: Options{Names: names, Budget: size("AGENTS.md") + size("sub/AGENTS.md") + size("sub/deep/CLAUDE.md") - 1}},
		{name: "smaller than the root file yields nothing", path: "sub/deep/file.txt", opts: Options{Names: names, Budget: size("AGENTS.md") - 1}},
	}
	fsyss := map[string]fs.FS{"os.DirFS": os.DirFS(root), "fstest.MapFS": treeFS(t, root)}
	for fsName, fsys := range fsyss {
		for _, tt := range tests {
			t.Run(fsName+"/"+tt.name, func(t *testing.T) {
				osOpts := tt.opts
				osOpts.Root = root
				want, err := Chain(abs(tt.path), osOpts)
				if err != nil {
					t.Fatal(err)
				}
				fsOpts := tt.opts
				fsOpts.FS = fsys
				got, err := Chain(tt.path, fsOpts)
				if err != nil {
					t.Fatal(err)
				}
				var wantFiles []File
				for _, f := range want.Files {
					wantFiles = append(wantFiles, File{Path: name(f.Path), Content: f.Content})
				}
				var wantOmitted []Omitted
				for _, o := range want.Omitted {
					wantOmitted = append(wantOmitted, Omitted{Path: name(o.Path), Size: o.Size, Reason: o.Reason, By: name(o.By)})
				}
				if !reflect.DeepEqual(got.Files, wantFiles) {
					t.Errorf("Files = %+v, want %+v", got.Files, wantFiles)
				}
				if !reflect.DeepEqual(got.Omitted, wantOmitted) {
					t.Errorf("Omitted = %+v, want %+v", got.Omitted, wantOmitted)
				}
				if g, w := Render(got.Files), Render(wantFiles); g != w {
					t.Errorf("Render() =\n%s\nwant\n%s", g, w)
				}
			})
		}
	}
}

// TestRenderFS: the golden render, walked through an FS, differs only
// in naming each file by its name in the FS.
func TestRenderFS(t *testing.T) {
	root := fixtureRoot(t)
	res, err := Chain("sub/deep/file.txt", Options{FS: treeFS(t, root), Names: []string{"AGENTS.md", "CLAUDE.md"}})
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "golden", "render.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(Render(res.Files), `path="`, `path="<root>/`)
	if got != string(want) {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

// TestChainFSRoot: Root is a name in the FS, the walk stops there, a
// path outside it is an error as on the OS, and a name that is not
// valid in an FS is an error rather than something to resolve.
func TestChainFSRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"AGENTS.md":                {Data: []byte("root\n")},
		"sub/AGENTS.md":            {Data: []byte("sub\n")},
		"sub/deep/AGENTS.md":       {Data: []byte("deep\n")},
		"sub/deep/file.txt":        {Data: []byte("file\n")},
		"subway/AGENTS.md":         {Data: []byte("subway\n")},
		"subway/station/README.md": {Data: []byte("readme\n")},
	}
	tests := []struct {
		name string
		path string
		opts Options
		want []string
		err  string
	}{
		{name: "empty root is the FS root", path: "sub/deep/file.txt", want: []string{"AGENTS.md", "sub/AGENTS.md", "sub/deep/AGENTS.md"}},
		{name: "dot root is the FS root", path: "sub/deep", opts: Options{Root: "."}, want: []string{"AGENTS.md", "sub/AGENTS.md", "sub/deep/AGENTS.md"}},
		{name: "sub-directory root stops the walk", path: "sub/deep/file.txt", opts: Options{Root: "sub"}, want: []string{"sub/AGENTS.md", "sub/deep/AGENTS.md"}},
		{name: "root is the start", path: "sub/deep", opts: Options{Root: "sub/deep"}, want: []string{"sub/deep/AGENTS.md"}},
		{name: "outside root", path: "other", opts: Options{Root: "sub"}, err: "is outside root"},
		{name: "above root", path: "sub", opts: Options{Root: "sub/deep"}, err: "is outside root"},
		{name: "a sibling sharing the prefix is outside", path: "subway/station", opts: Options{Root: "sub"}, err: "is outside root"},
		{name: "absolute path", path: "/sub", err: "not a valid name"},
		{name: "dot-dot path", path: "../sub", err: "not a valid name"},
		{name: "dot-slash path", path: "./sub", err: "not a valid name"},
		{name: "root with a trailing slash", path: "sub", opts: Options{Root: "sub/"}, err: "not a valid name"},
		{name: "a name that climbs out", path: "sub", opts: Options{Names: []string{"../../AGENTS.md"}}, err: "not a valid name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.FS = fsys
			res, err := Chain(tt.path, tt.opts)
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
			for _, f := range res.Files {
				if f.Content != string(fsys[f.Path].Data) {
					t.Errorf("%s content differs from the FS", f.Path)
				}
			}
		})
	}
}

// TestChainFSExtraIsAnOSPath: Extra is read from the OS whether or
// not FS is set, and keeps its absolute path, before the FS's chain.
func TestChainFSExtraIsAnOSPath(t *testing.T) {
	root := fixtureRoot(t)
	extra := filepath.Join(root, "sub", "deep", "CLAUDE.md")
	fsys := fstest.MapFS{"AGENTS.md": {Data: []byte("in the FS\n")}}
	res, err := Chain(".", Options{FS: fsys, Extra: []string{extra, filepath.Join(root, "nowhere.md")}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(extra)
	if err != nil {
		t.Fatal(err)
	}
	want := []File{{Path: extra, Content: string(data)}, {Path: "AGENTS.md", Content: "in the FS\n"}}
	if !reflect.DeepEqual(res.Files, want) {
		t.Errorf("Chain().Files = %+v, want %+v", res.Files, want)
	}
}

var errEscapes = errors.New("path escapes from parent")

// refusing is an FS that confines: opening the refused name fails as
// a link out of a confined FS does. It has Open alone, so fs.Stat and
// fs.ReadFile both come through it.
type refusing struct {
	fsys    fs.FS
	refused string
}

func (r refusing) Open(name string) (fs.File, error) {
	if name == r.refused {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errEscapes}
	}
	return r.fsys.Open(name)
}

// TestChainFSRefusedLink: a name the FS refuses, as a confining FS
// refuses a link that leads out of it, is an error naming the refusal,
// and never a file, a skipped file or something Chain resolves itself.
func TestChainFSRefusedLink(t *testing.T) {
	fsys := treeFS(t, fixtureRoot(t))
	for _, refused := range []string{"AGENTS.md", "sub/AGENTS.md", "sub/deep/file.txt", "shadow/AGENTS.md"} {
		t.Run(refused, func(t *testing.T) {
			path := "sub/deep/file.txt"
			if strings.HasPrefix(refused, "shadow/") {
				path = "shadow/inner"
			}
			res, err := Chain(path, Options{FS: refusing{fsys, refused}, Names: []string{"AGENTS.override.md", "AGENTS.md"}})
			if !errors.Is(err, errEscapes) {
				t.Fatalf("Chain() error = %v, want the refusal", err)
			}
			if len(res.Files) != 0 || len(res.Omitted) != 0 {
				t.Errorf("Chain() = %+v with an error", res)
			}
		})
	}
}

// TestChainOSRootRefusesALinkOut: through os.Root, the confining FS
// the standard library has, a project's AGENTS.md linking out of the
// project is an error, where the OS walk reads it through.
func TestChainOSRootRefusesALinkOut(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.md"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(dir, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "secret.md"), filepath.Join(proj, "AGENTS.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root, err := os.OpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	res, err := Chain(".", Options{FS: root.FS()})
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Chain() = %+v, %v; want an error refusing the link", res, err)
	}
	if len(res.Files) != 0 {
		t.Errorf("Chain() read %+v through a link out of the FS", res.Files)
	}
}

// recording is an FS that answers fs.Stat itself and records every
// name opened, so the names it records are the names fs.ReadFile read.
type recording struct {
	fstest.MapFS
	opened *[]string
}

func (r recording) Open(name string) (fs.File, error) {
	*r.opened = append(*r.opened, name)
	return r.MapFS.Open(name)
}

func (r recording) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(struct{ fs.FS }{r}, name)
}

// TestChainFSReadsOnlyIncludedFiles: as on the OS, a shadowed or
// over-budget file is stat'd for its size and never read.
func TestChainFSReadsOnlyIncludedFiles(t *testing.T) {
	root := fixtureRoot(t)
	var opened []string
	fsys := recording{treeFS(t, root), &opened}
	res, err := Chain("shadow/inner", Options{
		FS:     fsys,
		Names:  []string{"AGENTS.override.md", "AGENTS.md"},
		Budget: int64(len(fsys.MapFS["AGENTS.md"].Data) + len(fsys.MapFS["shadow/AGENTS.override.md"].Data)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(res.Files), []string{"AGENTS.md", "shadow/AGENTS.override.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Chain().Files = %v, want %v", got, want)
	}
	if len(res.Omitted) != 2 {
		t.Errorf("Chain().Omitted = %+v, want the shadowed and the over-budget file", res.Omitted)
	}
	slices.Sort(opened)
	if want := paths(res.Files); !reflect.DeepEqual(opened, want) {
		t.Errorf("opened %v, want only the included %v", opened, want)
	}
}
