package agentsmd

import (
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

// TestRenderedCost: the costs Chain charges against RenderedBudget add
// up to the length of Render's output, for paths that need escaping,
// content with and without a trailing newline, and empty content.
func TestRenderedCost(t *testing.T) {
	tests := [][]File{
		nil,
		{{Path: "AGENTS.md", Content: "rules\n"}},
		{{Path: "AGENTS.md", Content: "no newline"}},
		{{Path: "AGENTS.md", Content: ""}},
		{{Path: `/a/"b"&<c>`, Content: "x"}, {Path: "sub/AGENTS.md", Content: "y\n"}, {Path: "e", Content: ""}},
	}
	for _, files := range tests {
		var cost int64
		for i, f := range files {
			cost += renderedCost(i == 0, f.Path, int64(len(f.Content)), len(f.Content) == 0 || f.Content[len(f.Content)-1] != '\n')
		}
		if got := int64(len(Render(files))); cost != got {
			t.Errorf("costs of %+v = %d, want len(Render()) = %d", files, cost, got)
		}
	}
}

// TestChainRenderedBudget: for every budget from nothing to the whole
// rendering, the chain is the longest prefix whose rendering fits,
// every file after it is omitted over budget, and the rendering of
// the files returned is within the budget. It holds on the OS, with
// absolute paths, and through an FS whose names need escaping and
// whose files lack a trailing newline or are empty.
func TestChainRenderedBudget(t *testing.T) {
	root := fixtureRoot(t)
	fsys := fstest.MapFS{
		"AGENTS.md":                {Data: []byte("root\n")},
		"a&b/AGENTS.md":            {Data: []byte("no newline")},
		`a&b/"c"/AGENTS.md`:        {Data: []byte{}},
		`a&b/"c"/<d>/AGENTS.md`:    {Data: []byte("nearest\n")},
		`a&b/"c"/<d>/file.txt`:     {Data: []byte("file\n")},
		"unrelated/AGENTS.md":      {Data: []byte("unrelated\n")},
		"unrelated/deep/README.md": {Data: []byte("readme\n")},
	}
	cases := []struct {
		name string
		path string
		opts Options
	}{
		{
			name: "os",
			path: filepath.Join(root, "sub", "deep", "file.txt"),
			opts: Options{Root: root, Names: []string{"AGENTS.md", "CLAUDE.md"}, Extra: []string{filepath.Join(root, "shadow", "AGENTS.md")}},
		},
		{
			name: "fs",
			path: `a&b/"c"/<d>/file.txt`,
			opts: Options{FS: fsys},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			all, err := Chain(c.path, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(all.Files) < 3 {
				t.Fatalf("fixture chain has %d files, want at least 3", len(all.Files))
			}
			whole := int64(len(Render(all.Files)))
			for budget := int64(1); budget <= whole+1; budget++ {
				opts := c.opts
				opts.RenderedBudget = budget
				res, err := Chain(c.path, opts)
				if err != nil {
					t.Fatal(err)
				}
				n := len(res.Files)
				if !reflect.DeepEqual(res.Files, all.Files[:n]) && n > 0 {
					t.Fatalf("budget %d: Files = %v, want a prefix of %v", budget, paths(res.Files), paths(all.Files))
				}
				if got := int64(len(Render(res.Files))); got > budget {
					t.Fatalf("budget %d: rendering is %d bytes", budget, got)
				}
				if n < len(all.Files) {
					if next := int64(len(Render(all.Files[:n+1]))); next <= budget {
						t.Fatalf("budget %d: %s would have fitted (%d bytes)", budget, all.Files[n].Path, next)
					}
				}
				var omitted []Omitted
				for _, f := range all.Files[n:] {
					omitted = append(omitted, Omitted{Path: f.Path, Size: int64(len(f.Content)), Reason: OverBudget})
				}
				if !reflect.DeepEqual(res.Omitted, omitted) {
					t.Fatalf("budget %d: Omitted = %+v, want %+v", budget, res.Omitted, omitted)
				}
			}
		})
	}
}

// TestChainRenderedBudgetWithBudget: with both set, the first file
// over either ends the chain, so each still holds.
func TestChainRenderedBudgetWithBudget(t *testing.T) {
	fsys := fstest.MapFS{
		"AGENTS.md":     {Data: []byte("root\n")},
		"sub/AGENTS.md": {Data: []byte("sub\n")},
	}
	one := int64(len(Render([]File{{Path: "AGENTS.md", Content: "root\n"}})))
	both := int64(len(Render([]File{{Path: "AGENTS.md", Content: "root\n"}, {Path: "sub/AGENTS.md", Content: "sub\n"}})))
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{name: "both fit", opts: Options{Budget: 9, RenderedBudget: both}, want: []string{"AGENTS.md", "sub/AGENTS.md"}},
		{name: "the files' budget binds", opts: Options{Budget: 8, RenderedBudget: both}, want: []string{"AGENTS.md"}},
		{name: "the rendered budget binds", opts: Options{Budget: 9, RenderedBudget: both - 1}, want: []string{"AGENTS.md"}},
		{name: "the wrapper alone does not fit", opts: Options{RenderedBudget: one - 1}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.FS = fsys
			res, err := Chain("sub", tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := paths(res.Files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Chain().Files = %v, want %v", got, tt.want)
			}
			if len(res.Files)+len(res.Omitted) != 2 {
				t.Errorf("Chain().Omitted = %+v, want the rest", res.Omitted)
			}
		})
	}
}

// TestChainRenderedBudgetChargesBytesRead: the rendered budget, like
// Budget, is charged on the bytes a file reads.
func TestChainRenderedBudgetChargesBytesRead(t *testing.T) {
	fsys := misreporting{
		MapFS: fstest.MapFS{"AGENTS.md": {Data: []byte("much longer than it says\n")}},
		sizes: map[string]int64{"AGENTS.md": 1},
	}
	budget := int64(len(Render([]File{{Path: "AGENTS.md", Content: "x"}})))
	res, err := Chain(".", Options{FS: fsys, RenderedBudget: budget})
	if err != nil {
		t.Fatal(err)
	}
	want := []Omitted{{Path: "AGENTS.md", Size: 25, Reason: OverBudget}}
	if len(res.Files) != 0 || !reflect.DeepEqual(res.Omitted, want) {
		t.Errorf("Chain() = %+v, want AGENTS.md omitted with the 25 bytes it read", res)
	}
}
