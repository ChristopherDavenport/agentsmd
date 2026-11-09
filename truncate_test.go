package agentsmd

import (
	"errors"
	"io/fs"
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
			"big/AGENTS.md":      {Data: []byte(strings.Repeat("x\n", 200))},
			"small/AGENTS.md":    {Data: []byte(strings.Repeat("x\n", 200))}, // stats as 10
			"lies/AGENTS.md":     {Data: []byte(sub)},                        // stats as 1000
		},
		sizes: map[string]int64{"lies/AGENTS.md": 1000, "small/AGENTS.md": 10},
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
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 200, Kept: 8, Reason: Truncated}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			// One byte short of "one\ntwo\n" and its marker: the cut
			// goes back a whole line, not a byte.
			name:    "the marker counts against the budget",
			path:    "sub/deep",
			opts:    Options{Budget: 5 + 8 + m200 - 1, Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"sub/AGENTS.md", "one\n" + mark(204)}},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 204, Kept: 4, Reason: Truncated}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
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
			// leaves it out unread; under Truncate it is read only as
			// far as could fit, so the bytes left out are counted from
			// its stat: 1000 less the 8 kept.
			name:    "a file that stats over the budget is cut, counted from its stat",
			path:    "lies",
			opts:    Options{Budget: 5 + 8 + int64(len(marker("lies/AGENTS.md", 992))), Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"lies/AGENTS.md", "one\ntwo\n" + marker("lies/AGENTS.md", 992)}},
			omitted: []Omitted{{Path: "lies/AGENTS.md", Size: 992, Kept: 8, Reason: Truncated}},
		},
		{
			name:    "off: a file that stats over the budget is left out unread",
			path:    "lies",
			opts:    Options{Budget: 5 + 8 + int64(len(marker("lies/AGENTS.md", 992)))},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "lies/AGENTS.md", Size: 1000, Reason: OverBudget}},
		},
		{
			// It stats over the budget, so it is not read and MaxBytes
			// is not checked.
			name:    "off: a file over MaxBytes and the budget is left out",
			path:    "big",
			opts:    Options{Budget: 105, MaxBytes: 300},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "big/AGENTS.md", Size: 400, Reason: OverBudget}},
		},
		{
			// Only the 101 bytes that could matter are read, short of
			// MaxBytes, so it is cut, not an error: 36 bytes and a
			// 64-byte marker fit the 100 left.
			name:    "a file over MaxBytes and the budget is cut, not an error",
			path:    "big",
			opts:    Options{Budget: 105, MaxBytes: 300, Truncate: true},
			files:   []File{{"AGENTS.md", root}, {"big/AGENTS.md", strings.Repeat("x\n", 18) + marker("big/AGENTS.md", 364)}},
			omitted: []Omitted{{Path: "big/AGENTS.md", Size: 364, Kept: 36, Reason: Truncated}},
		},
		{
			// MaxBytes is reached before the room is, so the file is left
			// out as it would be without Truncate, not an error.
			name:    "a file over MaxBytes with room to spare is left out, not an error",
			path:    "big",
			opts:    Options{Budget: 305, MaxBytes: 39, Truncate: true},
			files:   []File{{"AGENTS.md", root}},
			omitted: []Omitted{{Path: "big/AGENTS.md", Size: 400, Reason: OverBudget}},
		},
		{
			// It stats within the budget, so it is read whole and held
			// to MaxBytes, with Truncate as without.
			name: "a file whose stat fits and reads over MaxBytes is an error",
			path: "small",
			opts: Options{Budget: 1000, MaxBytes: 39, Truncate: true},
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
			checkKept(t, res)
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
				if o.Kept != int64(kept) {
					t.Fatalf("budget %d: Kept = %d, want %d", budget, o.Kept, kept)
				}
				checkKept(t, res)
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

// lineFile is "line\n" over and over, without end.
type lineFile struct {
	fs.File
	off int
}

func (l *lineFile) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = "line\n"[l.off%5]
		l.off++
	}
	return len(p), nil
}

// countingFile counts the bytes read from it.
type countingFile struct {
	fs.File
	n *int64
}

func (c countingFile) Read(p []byte) (int, error) {
	n, err := c.File.Read(p)
	*c.n += int64(n)
	return n, err
}

// endlessCounted is an FS whose AGENTS.md never ends, stats as size,
// and counts the bytes read from it into read.
type endlessCounted struct {
	fstest.MapFS
	size int64
	read *int64
}

func (e endlessCounted) Stat(name string) (fs.FileInfo, error) {
	info, err := e.MapFS.Stat(name)
	if err == nil && name == "AGENTS.md" {
		return sizedInfo{info, e.size}, nil
	}
	return info, err
}

func (e endlessCounted) Open(name string) (fs.File, error) {
	f, err := e.MapFS.Open(name)
	if err != nil || name != "AGENTS.md" {
		return f, err
	}
	return countingFile{&lineFile{File: f}, e.read}, nil
}

// TestChainTruncateEndless: under Truncate a file whose stat is over
// the budget is read no further than one byte past the room left,
// under either budget, so a file that never ends is cut and Chain
// returns without error, counting the bytes left out from the stat.
// Without Truncate it is left out unread.
func TestChainTruncateEndless(t *testing.T) {
	const size = 1 << 40
	element := renderedCost(true, "AGENTS.md", 0, false)
	tests := []struct {
		name string
		opts Options
		room int64
	}{
		{name: "Budget", opts: Options{Budget: 200}, room: 200},
		{name: "RenderedBudget", opts: Options{RenderedBudget: element + 200}, room: 200},
		{name: "both, Budget tighter", opts: Options{Budget: 150, RenderedBudget: element + 200}, room: 150},
		{name: "both, RenderedBudget tighter", opts: Options{Budget: 300, RenderedBudget: element + 120}, room: 120},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var read int64
			tt.opts.FS = endlessCounted{fstest.MapFS{"AGENTS.md": {Data: []byte("x")}}, size, &read}
			tt.opts.MaxBytes = 1 << 10

			off, err := Chain(".", tt.opts)
			if err != nil {
				t.Fatalf("without Truncate: %v", err)
			}
			if want := []Omitted{{Path: "AGENTS.md", Size: size, Reason: OverBudget}}; len(off.Files) != 0 || !reflect.DeepEqual(off.Omitted, want) || read != 0 {
				t.Fatalf("without Truncate: %+v, %d bytes read; want it left out unread", off, read)
			}

			tt.opts.Truncate = true
			res, err := Chain(".", tt.opts)
			if err != nil {
				t.Fatalf("Chain() error = %v", err)
			}
			if read > tt.room+1 {
				t.Errorf("read %d bytes, want no more than %d", read, tt.room+1)
			}
			if len(res.Files) != 1 || len(res.Omitted) != 1 || res.Omitted[0].Reason != Truncated {
				t.Fatalf("Chain() = %+v, want AGENTS.md cut", res)
			}
			kept := size - res.Omitted[0].Size
			if res.Omitted[0].Kept != kept {
				t.Errorf("Kept = %d, want %d", res.Omitted[0].Kept, kept)
			}
			checkKept(t, res)
			want := strings.Repeat("line\n", int(kept/5)) + marker("AGENTS.md", size-kept)
			if kept <= 0 || kept%5 != 0 || res.Files[0].Content != want {
				t.Errorf("Content = %q, Omitted = %+v; want whole lines and the marker", res.Files[0].Content, res.Omitted)
			}
			// The cut is the longest that fits: one more line does not.
			if got := int64(len(res.Files[0].Content)); got > tt.room || got+5 <= tt.room {
				t.Errorf("%d bytes with the marker, room %d: want the longest cut that fits", got, tt.room)
			}
			if got := int64(len(Render(res.Files))); tt.opts.RenderedBudget > 0 && got > tt.opts.RenderedBudget {
				t.Errorf("rendering %d bytes, over %d", got, tt.opts.RenderedBudget)
			}
		})
	}
}

// checkKept fails t unless every Truncated file in res.Omitted reports
// as Kept the bytes of its content ahead of its marker, and every other
// omission reports none.
func checkKept(t *testing.T, res Result) {
	t.Helper()
	content := map[string]string{}
	for _, f := range res.Files {
		content[f.Path] = f.Content
	}
	for _, o := range res.Omitted {
		want := int64(0)
		if o.Reason == Truncated {
			c, ok := content[o.Path]
			if !ok {
				t.Errorf("%s is Truncated but not in Files", o.Path)
				continue
			}
			want = int64(len(c) - len(marker(o.Path, o.Size)))
		}
		if o.Kept != want {
			t.Errorf("%s (%s): Kept = %d, want %d", o.Path, o.Reason, o.Kept, want)
		}
	}
}
