package agentsmd

import (
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
)

// misreporting is an FS whose Stat reports the sizes in sizes rather
// than the bytes a file reads, as an FS that expands imports does, or
// as a file that grows or shrinks between the stat and the read does.
type misreporting struct {
	fstest.MapFS
	sizes map[string]int64
}

func (m misreporting) Stat(name string) (fs.FileInfo, error) {
	info, err := m.MapFS.Stat(name)
	if size, ok := m.sizes[name]; ok && err == nil {
		return sizedInfo{info, size}, nil
	}
	return info, err
}

type sizedInfo struct {
	fs.FileInfo
	size int64
}

func (s sizedInfo) Size() int64 { return s.size }

// TestChainBoundsChargeBytesRead: MaxBytes and Budget apply to the
// bytes a file reads, not the size its Stat reported. A file that
// reads over MaxBytes is an error; one that reads past the budget left
// is omitted with the size it read and ends the chain.
func TestChainBoundsChargeBytesRead(t *testing.T) {
	data := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'x'
		}
		b[n-1] = '\n'
		return b
	}
	fsys := misreporting{
		MapFS: fstest.MapFS{
			"AGENTS.md":          {Data: data(10)},
			"sub/AGENTS.md":      {Data: data(50)}, // stats as 10
			"sub/deep/AGENTS.md": {Data: data(5)},
			"shrunk/AGENTS.md":   {Data: data(10)}, // stats as 50
		},
		sizes: map[string]int64{"sub/AGENTS.md": 10, "shrunk/AGENTS.md": 50},
	}
	tests := []struct {
		name    string
		path    string
		opts    Options
		want    []string
		omitted []Omitted
		err     error
	}{
		{
			name: "no bounds: the bytes read are returned",
			path: "sub/deep",
			want: []string{"AGENTS.md", "sub/AGENTS.md", "sub/deep/AGENTS.md"},
		},
		{
			name: "reads over MaxBytes though it stats under",
			path: "sub/deep",
			opts: Options{MaxBytes: 20},
			err:  ErrTooLarge,
		},
		{
			name: "reads exactly MaxBytes",
			path: "sub/deep",
			opts: Options{MaxBytes: 50},
			want: []string{"AGENTS.md", "sub/AGENTS.md", "sub/deep/AGENTS.md"},
		},
		{
			// It stats as fitting, so it is read; it reads past the 20
			// bytes left, so it is omitted with the 50 it read, and the
			// chain ends there although the next file would fit.
			name:    "reads past the budget though it stats within",
			path:    "sub/deep",
			opts:    Options{Budget: 30},
			want:    []string{"AGENTS.md"},
			omitted: []Omitted{{Path: "sub/AGENTS.md", Size: 50, Reason: OverBudget}, {Path: "sub/deep/AGENTS.md", Size: 5, Reason: OverBudget}},
		},
		{
			name: "reads within the budget exactly",
			path: "sub/deep",
			opts: Options{Budget: 65},
			want: []string{"AGENTS.md", "sub/AGENTS.md", "sub/deep/AGENTS.md"},
		},
		{
			// A reported size decides only whether to read: one over the
			// budget is left out unread, with the size reported.
			name:    "stats past the budget though it would read within",
			path:    "shrunk",
			opts:    Options{Budget: 30},
			want:    []string{"AGENTS.md"},
			omitted: []Omitted{{Path: "shrunk/AGENTS.md", Size: 50, Reason: OverBudget}},
		},
		{
			// MaxBytes is judged on what a file reads, so a file that
			// stats over it and reads within it is included.
			name: "stats over MaxBytes though it reads within",
			path: "shrunk",
			opts: Options{MaxBytes: 20},
			want: []string{"AGENTS.md", "shrunk/AGENTS.md"},
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
			if got := paths(res.Files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Chain().Files = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(res.Omitted, tt.omitted) {
				t.Errorf("Chain().Omitted = %+v, want %+v", res.Omitted, tt.omitted)
			}
			var total int64
			for _, f := range res.Files {
				if f.Content != string(fsys.MapFS[f.Path].Data) {
					t.Errorf("%s content differs from the FS", f.Path)
				}
				total += int64(len(f.Content))
			}
			if tt.opts.Budget > 0 && total > tt.opts.Budget {
				t.Errorf("total %d exceeds budget %d", total, tt.opts.Budget)
			}
		})
	}
}

// endless is a file that never ends, as a device or an FS generating
// text might be.
type endless struct{ fs.File }

func (endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

type endlessFS struct{ fstest.MapFS }

func (e endlessFS) Open(name string) (fs.File, error) {
	f, err := e.MapFS.Open(name)
	if err != nil || name != "AGENTS.md" {
		return f, err
	}
	return endless{f}, nil
}

// TestChainReadsNoFurtherThanMaxBytes: a file over MaxBytes is refused
// after one byte past it, not read through.
func TestChainReadsNoFurtherThanMaxBytes(t *testing.T) {
	fsys := endlessFS{fstest.MapFS{"AGENTS.md": {Data: []byte("short\n")}}}
	_, err := Chain(".", Options{FS: fsys, MaxBytes: 1 << 10})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Chain() error = %v, want ErrTooLarge", err)
	}
}
