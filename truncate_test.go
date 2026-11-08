package agentsmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// TestChainTruncate: under Truncate the first file that does not fit is
// cut at the last line boundary where it fits with its marker, is
// reported as Truncated with the bytes cut off, and ends the chain;
// without Truncate, or when not even the marker fits, it is left out
// whole as before.
func TestChainTruncate(t *testing.T) {
	const root, deep = "root\n", "deep\n"
	// 208 bytes, with line boundaries after 4, 8 and 108, and a last
	// line with no newline.
	sub := "one\ntwo\n" + strings.Repeat("t", 99) + "\n" + strings.Repeat("n", 100)
	fsys := misreporting{
		MapFS: fstest.MapFS{
			"AGENTS.md":          {Data: []byte(root)},
			"sub/AGENTS.md":      {Data: []byte(sub)},
			"sub/deep/AGENTS.md": {Data: []byte(deep)},
			"big/AGENTS.md":      {Data: []byte(strings.Repeat("x\n", 20))},
			"lies/AGENTS.md":     {Data: []byte(sub)}, // stats as 1000
		},
		sizes: map[string]int64{"lies/AGENTS.md": 1000},
	}
	mark := func(left int) string { return marker("sub/AGENTS.md", int64(left)) }
	m200 := int64(len(mark(200))) // the marker for the 200 bytes after "one\ntwo\n"
	tests := []struct {
		name    string
		path    string
		opts    Options
		files   []File
		omitted []Omitted
		err     error
	}{
		{
			name:    "off: the file and every later one are left out whole",
			path:    "sub/deep",
			opts:    Options{Budget: 5 + 8 + m200},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 208, Reason: OverBudget}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			name:    "cut at the last line boundary that fits with the marker",
			path:    "sub/deep",
			opts:    Options{Budget: 5 + 8 + m200, Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"sub/AGENTS.md", "one\ntwo\n" + mark(200)}},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 200, Reason: Truncated}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			// One byte short of "one\ntwo\n" and its marker: the cut
			// goes back a whole line, not a byte.
			name:    "the marker counts against the budget",
			path:    "sub/deep",
			opts:    Options{Budget: 5 + 8 + m200 - 1, Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"sub/AGENTS.md", "one\n" + mark(204)}},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 204, Reason: Truncated}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			name:    "the marker alone, when no line fits beside it",
			path:    "sub/deep",
			opts:    Options{Budget: 5 + int64(len(mark(208))), Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"sub/AGENTS.md", mark(208)}},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 208, Reason: Truncated}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			name:    "a budget too small for the marker leaves the file out whole",
			path:    "sub/deep",
			opts:    Options{Budget: 5 + int64(len(mark(208))) - 1, Truncate: true},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 208, Reason: OverBudget}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			name:  "a file that fits is not cut",
			path:  "sub/deep",
			opts:  Options{Budget: 5 + 208 + 5, Truncate: true},
			files: []File{{"AGENTS.md", root}, {"sub/AGENTS.md", sub}, {"sub/deep/AGENTS.md", deep}},
		},
		{
			// It stats far over the budget, which without Truncate
			// leaves it out unread; under Truncate it is read and cut on
			// the bytes it reads.
			name:    "a file that stats over the budget is read and cut",
			path:    "lies",
			opts:    Options{Budget: 5 + 8 + int64(len(marker("lies/AGENTS.md", 200))), Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"lies/AGENTS.md", "one\ntwo\n" + marker("lies/AGENTS.md", 200)}},
			omitted: []Omitted{{Path: "lies/AGENTS.md", Size: 200, Reason: Truncated}},
		},
		{
			name:    "off: a file that stats over the budget is left out unread",
			path:    "lies",
			opts:    Options{Budget: 5 + 8 + int64(len(marker("lies/AGENTS.md", 200)))},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "lies/AGENTS.md", Size: 1000, Reason: OverBudget}},
		},
		{
			// It stats over the budget, so it is not read and MaxBytes
			// is not checked.
			name:    "off: a file over MaxBytes and the budget is left out",
			path:    "big",
			opts:    Options{Budget: 10, MaxBytes: 39},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "big/AGENTS.md", Size: 40, Reason: OverBudget}},
		},
		{
			// The file to cut is read, so it is held to MaxBytes.
			name: "MaxBytes stays an error",
			path: "big",
			opts: Options{Budget: 10, MaxBytes: 39, Truncate: true},
			err:  ErrTooLarge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.FS = fsys
			res, err := Chain(tt.path, tt.opts)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("Chain() error = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(res.Files, tt.files) {
				t.Errorf("Chain().Files = %q, want %q", res.Files, tt.files)
			}
			if !reflect.DeepEqual(res.Omitted, tt.omitted) {
				t.Errorf("Chain().Omitted = %+v, want %+v", res.Omitted, tt.omitted)
			}
			var total int64
			for _, f := range res.Files {
				total += int64(len(f.Content))
			}
			if total > tt.opts.Budget {
				t.Errorf("total %d exceeds budget %d", total, tt.opts.Budget)
			}
		})
	}
}

// TestChainTruncateSweep: for every Budget, and every RenderedBudget,
// from nothing to the whole chain, Truncate returns the files that fit
// whole, then at most one cut file: a prefix of it ending at a line
// boundary followed by its marker, the longest that fits, reported as
// Truncated with the bytes cut off. Every file after it is left out
// whole, and the bound holds. Where nothing is cut, the result is the
// one without Truncate.
func TestChainTruncateSweep(t *testing.T) {
	fsys := fstest.MapFS{
		"AGENTS.md":                {Data: []byte("root rules\nsecond line\n")},
		"a&b/AGENTS.md":            {Data: []byte(strings.Repeat("a line of text\n", 8) + "no newline")},
		`a&b/"c"/AGENTS.md`:        {Data: []byte{}},
		`a&b/"c"/<d>/AGENTS.md`:    {Data: []byte("nearest\none more\n")},
		`a&b/"c"/<d>/file.txt`:     {Data: []byte("file\n")},
		"unrelated/AGENTS.md":      {Data: []byte("unrelated\n")},
		"unrelated/deep/README.md": {Data: []byte("readme\n")},
	}
	const path = `a&b/"c"/<d>/file.txt`
	all, err := Chain(path, Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	whole := map[string]File{}
	for _, f := range all.Files {
		whole[f.Path] = f
	}
	size := func(files []File) int64 {
		var n int64
		for _, f := range files {
			n += int64(len(f.Content))
		}
		return n
	}
	bounds := []struct {
		name string
		set  func(o *Options, b int64)
		cost func(files []File) int64
	}{
		{"Budget", func(o *Options, b int64) { o.Budget = b }, size},
		{"RenderedBudget", func(o *Options, b int64) { o.RenderedBudget = b }, func(files []File) int64 { return int64(len(Render(files))) }},
	}
	for _, bound := range bounds {
		t.Run(bound.name, func(t *testing.T) {
			cuts := 0
			for budget := int64(1); budget <= bound.cost(all.Files)+1; budget++ {
				opts := Options{FS: fsys}
				bound.set(&opts, budget)
				plain, err := Chain(path, opts)
				if err != nil {
					t.Fatal(err)
				}
				opts.Truncate = true
				res, err := Chain(path, opts)
				if err != nil {
					t.Fatal(err)
				}
				if got := bound.cost(res.Files); got > budget {
					t.Fatalf("budget %d: %d bytes", budget, got)
				}
				if len(res.Omitted) == 0 || res.Omitted[0].Reason != Truncated {
					if !reflect.DeepEqual(res, plain) {
						t.Fatalf("budget %d: nothing cut, but %+v differs from %+v without Truncate", budget, res, plain)
					}
					continue
				}
				cuts++
				n := len(plain.Files)
				if len(res.Files) != n+1 || !reflect.DeepEqual(res.Files[:n], plain.Files) {
					t.Fatalf("budget %d: Files = %v, want %v and one cut file", budget, paths(res.Files), paths(plain.Files))
				}
				f := res.Files[n]
				data := whole[f.Path].Content
				o := res.Omitted[0]
				kept := len(data) - int(o.Size)
				if o.Path != f.Path || kept < 0 || f.Content != data[:kept]+marker(f.Path, o.Size) {
					t.Fatalf("budget %d: cut file %q, omitted %+v, from %q", budget, f.Content, o, data)
				}
				if kept > 0 && data[kept-1] != '\n' {
					t.Fatalf("budget %d: cut %q mid-line", budget, data[:kept])
				}
				if i := strings.IndexByte(data[kept:], '\n'); i >= 0 && kept+i+1 < len(data) {
					longer := append(append([]File{}, res.Files[:n]...), File{f.Path, data[:kept+i+1] + marker(f.Path, int64(len(data)-kept-i-1))})
					if bound.cost(longer) <= budget {
						t.Fatalf("budget %d: %q would have fitted", budget, longer[n].Content)
					}
				}
				if want := plain.Omitted[1:]; !reflect.DeepEqual(res.Omitted[1:], want) {
					t.Fatalf("budget %d: later files %+v, want %+v", budget, res.Omitted[1:], want)
				}
			}
			if cuts == 0 {
				t.Fatal("no budget cut a file")
			}
		})
	}
}
