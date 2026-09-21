package agentsmd

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden file under testdata/golden")

func fixtureRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "tree"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func paths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func TestChain(t *testing.T) {
	root := fixtureRoot(t)
	rel := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	tests := []struct {
		name    string
		path    string
		opts    Options
		want    []string
		omitted []Omitted
		err     string
	}{
		{
			name: "file, default names",
			path: rel("sub", "deep", "file.txt"),
			opts: Options{Root: root},
			want: []string{rel("AGENTS.md"), rel("sub", "AGENTS.md")},
		},
		{
			name: "directory",
			path: rel("sub", "deep"),
			opts: Options{Root: root},
			want: []string{rel("AGENTS.md"), rel("sub", "AGENTS.md")},
		},
		{
			name: "file about to be created",
			path: rel("sub", "deep", "new.go"),
			opts: Options{Root: root},
			want: []string{rel("AGENTS.md"), rel("sub", "AGENTS.md")},
		},
		{
			name: "two names, nearest last",
			path: rel("sub", "deep", "file.txt"),
			opts: Options{Root: root, Names: []string{"AGENTS.md", "CLAUDE.md"}},
			want: []string{rel("AGENTS.md"), rel("sub", "AGENTS.md"), rel("sub", "deep", "CLAUDE.md")},
		},
		{
			// One file per directory: the first of Names found wins
			// and the rest are not read, so an override shadows the
			// committed file...
			name:    "override shadows per directory",
			path:    rel("shadow", "inner"),
			opts:    Options{Root: root, Names: []string{"AGENTS.override.md", "AGENTS.md"}},
			want:    []string{rel("AGENTS.md"), rel("shadow", "AGENTS.override.md"), rel("shadow", "inner", "AGENTS.md")},
			omitted: []Omitted{{Path: rel("shadow", "AGENTS.md"), Size: 74, Reason: Shadowed, By: rel("shadow", "AGENTS.override.md")}},
		},
		{
			// A shadowed file is not read, so its size is reported and
			// not held to MaxBytes.
			name:    "shadowed file over MaxBytes is not an error",
			path:    rel("shadow", "inner"),
			opts:    Options{Root: root, Names: []string{"AGENTS.override.md", "AGENTS.md"}, MaxBytes: 72},
			want:    []string{rel("AGENTS.md"), rel("shadow", "AGENTS.override.md"), rel("shadow", "inner", "AGENTS.md")},
			omitted: []Omitted{{Path: rel("shadow", "AGENTS.md"), Size: 74, Reason: Shadowed, By: rel("shadow", "AGENTS.override.md")}},
		},
		{
			// ...and the order of Names, not the order on disk, says
			// which.
			name:    "names are a preference order",
			path:    rel("shadow", "inner"),
			opts:    Options{Root: root, Names: []string{"AGENTS.md", "AGENTS.override.md"}},
			want:    []string{rel("AGENTS.md"), rel("shadow", "AGENTS.md"), rel("shadow", "inner", "AGENTS.md")},
			omitted: []Omitted{{Path: rel("shadow", "AGENTS.override.md"), Size: 72, Reason: Shadowed, By: rel("shadow", "AGENTS.md")}},
		},
		{
			name: "root only",
			path: rel("other"),
			opts: Options{Root: root},
			want: []string{rel("AGENTS.md")},
		},
		{
			name: "root below the start cuts the chain",
			path: rel("sub", "deep", "file.txt"),
			opts: Options{Root: rel("sub")},
			want: []string{rel("sub", "AGENTS.md")},
		},
		{
			// Extra comes before the chain, so the nearest file in the
			// tree is still the last text the model reads. A missing
			// path, and a directory of that name, are skipped.
			name: "extra first, missing skipped",
			path: rel("other"),
			opts: Options{Root: root, Extra: []string{rel("sub", "deep", "CLAUDE.md"), rel("nowhere.md"), rel("sub")}},
			want: []string{rel("sub", "deep", "CLAUDE.md"), rel("AGENTS.md")},
		},
		{
			name: "outside root",
			path: filepath.Dir(root),
			opts: Options{Root: root},
			err:  "is outside root",
		},
		{
			name: "too large",
			path: rel("sub"),
			opts: Options{Root: root, MaxBytes: 10},
			err:  ErrTooLarge.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			if !reflect.DeepEqual(res.Omitted, tt.omitted) {
				t.Errorf("Chain().Omitted = %+v, want %+v", res.Omitted, tt.omitted)
			}
			for _, f := range res.Files {
				data, err := os.ReadFile(f.Path)
				if err != nil || string(data) != f.Content {
					t.Errorf("%s content differs from disk", f.Path)
				}
			}
		})
	}
}

// TestChainBudget: the budget is a total, a file that would exceed it
// ends the chain, nothing is cut short, running out is not an error,
// and every file the chain would have included is reported.
func TestChainBudget(t *testing.T) {
	root := fixtureRoot(t)
	rel := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	size := func(parts ...string) int64 {
		info, err := os.Stat(rel(parts...))
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	rootSize, subSize, deepSize, innerSize := size("AGENTS.md"), size("sub", "AGENTS.md"), size("sub", "deep", "CLAUDE.md"), size("shadow", "inner", "AGENTS.md")
	if rootSize >= innerSize {
		t.Fatalf("fixture sizes: root (%d) must be smaller than inner (%d), so a budget that stops the chain at inner would have fitted root", rootSize, innerSize)
	}
	names := []string{"AGENTS.md", "CLAUDE.md"}
	over := func(parts ...string) Omitted {
		return Omitted{Path: rel(parts...), Size: size(parts...), Reason: OverBudget}
	}
	tests := []struct {
		name    string
		opts    Options
		want    []string
		omitted []Omitted
	}{
		{
			name: "zero is no budget",
			opts: Options{Root: root, Names: names},
			want: []string{rel("AGENTS.md"), rel("sub", "AGENTS.md"), rel("sub", "deep", "CLAUDE.md")},
		},
		{
			name: "all three fit exactly",
			opts: Options{Root: root, Names: names, Budget: rootSize + subSize + deepSize},
			want: []string{rel("AGENTS.md"), rel("sub", "AGENTS.md"), rel("sub", "deep", "CLAUDE.md")},
		},
		{
			name:    "one byte short drops the nearest",
			opts:    Options{Root: root, Names: names, Budget: rootSize + subSize + deepSize - 1},
			want:    []string{rel("AGENTS.md"), rel("sub", "AGENTS.md")},
			omitted: []Omitted{over("sub", "deep", "CLAUDE.md")},
		},
		{
			// The extra file does not fit; the smaller root file after
			// it would, but the chain stops rather than skips, and
			// every file it would have included is reported.
			name:    "the chain stops at the first file that does not fit",
			opts:    Options{Root: root, Names: names, Extra: []string{rel("shadow", "inner", "AGENTS.md")}, Budget: innerSize - 1},
			want:    []string{},
			omitted: []Omitted{over("shadow", "inner", "AGENTS.md"), over("AGENTS.md"), over("sub", "AGENTS.md"), over("sub", "deep", "CLAUDE.md")},
		},
		{
			name:    "smaller than the root file yields nothing",
			opts:    Options{Root: root, Names: names, Budget: rootSize - 1},
			want:    []string{},
			omitted: []Omitted{over("AGENTS.md"), over("sub", "AGENTS.md"), over("sub", "deep", "CLAUDE.md")},
		},
		{
			// A file left out for budget is not read, so MaxBytes
			// does not apply to it.
			name:    "an omitted file over MaxBytes is not an error",
			opts:    Options{Root: root, Names: names, Budget: rootSize, MaxBytes: rootSize},
			want:    []string{rel("AGENTS.md")},
			omitted: []Omitted{over("sub", "AGENTS.md"), over("sub", "deep", "CLAUDE.md")},
		},
		{
			// Extra is the first charge on the budget, as it is the
			// first text in the prompt, so what a tight budget drops
			// is the nearest file and not the user's own.
			name:    "extra shares the budget and comes first",
			opts:    Options{Root: rel("sub", "deep"), Names: names, Extra: []string{rel("AGENTS.md")}, Budget: deepSize + rootSize - 1},
			want:    []string{rel("AGENTS.md")},
			omitted: []Omitted{over("sub", "deep", "CLAUDE.md")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Chain(rel("sub", "deep", "file.txt"), tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := paths(res.Files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Chain().Files = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(res.Omitted, tt.omitted) {
				t.Errorf("Chain().Omitted = %+v, want %+v", res.Omitted, tt.omitted)
			}
			var total int64
			for _, f := range res.Files {
				data, err := os.ReadFile(f.Path)
				if err != nil || string(data) != f.Content {
					t.Errorf("%s content differs from disk", f.Path)
				}
				total += int64(len(f.Content))
			}
			if tt.opts.Budget > 0 && total > tt.opts.Budget {
				t.Errorf("total %d exceeds budget %d", total, tt.opts.Budget)
			}
		})
	}
}

func TestChainToFilesystemRoot(t *testing.T) {
	// With no Root, the walk reaches the file system root and stops.
	res, err := Chain(t.TempDir(), Options{Names: []string{"no-such-file-agentskill-test.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 0 || len(res.Omitted) != 0 {
		t.Errorf("Chain() = %+v, want none", res)
	}
}

func TestChainSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real.md")
	if err := os.WriteFile(target, []byte("linked rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "proj")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	res, err := Chain(dir, Options{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Path != link || res.Files[0].Content != "linked rules\n" {
		t.Errorf("Chain() = %+v, want the link read through", res.Files)
	}
}

func TestRender(t *testing.T) {
	root := fixtureRoot(t)
	res, err := Chain(filepath.Join(root, "sub", "deep", "file.txt"), Options{Root: root, Names: []string{"AGENTS.md", "CLAUDE.md"}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(Render(res.Files), root, "<root>")
	golden := filepath.Join("testdata", "golden", "render.txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test . -update)", err)
	}
	if got != string(want) {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
	if Render(nil) != "" {
		t.Error("Render(nil) is not empty")
	}
	one := Render([]File{{Path: `/a/"b"&<c>`, Content: "x"}})
	if !strings.Contains(one, `path="/a/&quot;b&quot;&amp;&lt;c&gt;"`) || !strings.HasSuffix(one, "x\n</project_instructions>\n</project_context>") {
		t.Errorf("Render() = %q", one)
	}
}

func TestErrTooLargeIsWrapped(t *testing.T) {
	root := fixtureRoot(t)
	_, err := Chain(root, Options{Root: root, MaxBytes: 1})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", err)
	}
}

func TestReasonString(t *testing.T) {
	for r, want := range map[Reason]string{OverBudget: "over budget", Shadowed: "shadowed", Reason(7): "reason(7)"} {
		if got := r.String(); got != want {
			t.Errorf("Reason(%d).String() = %q, want %q", int(r), got, want)
		}
	}
}
