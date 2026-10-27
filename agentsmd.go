// Package agentsmd finds and renders the instruction files of the
// AGENTS.md convention: the files that apply at a path are the ones in
// its directory and every ancestor, one per directory, and the nearest
// takes precedence.
//
// [Chain] collects them farthest first and nearest last, so that when
// a product includes every file, later text refines earlier text, and
// says which files it found and left out, so a product can record or
// show exactly what the model was given and what it was not. Explicit
// paths, such as the user's own file outside the tree, come before the
// chain, since the nearest file is the one that wins. The walk reads
// the OS file system, or an [fs.FS] a product supplies in
// [Options.FS] when the project is somewhere else, such as a container
// or a remote workspace. [Render] wraps
// the files the way pi renders project context, one
// project_instructions element per file inside project_context. The
// package imports the standard library alone.
package agentsmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultMaxBytes is the size above which a file is an error.
const DefaultMaxBytes = 1 << 20

// DefaultNames is the file name Chain looks for when Options.Names is
// empty.
var DefaultNames = []string{"AGENTS.md"}

// Options configure [Chain].
type Options struct {
	// FS, when set, is the file system the walk reads: the path given
	// to Chain, Root and every directory of the chain are names in it,
	// valid by [fs.ValidPath] ("." is its root), directories are
	// listed with [fs.ReadDir], and files are stat'd with [fs.Stat]
	// and read through Open. The walk never leaves FS: it only asks
	// FS for names it builds below "." and does not resolve links
	// itself, so an FS that confines, such as
	// [os.Root.FS], keeps its confinement, and an error it returns,
	// such as a link refused for leaving it, is Chain's error. Only an
	// [fs.ErrNotExist] means no file. Nil means the OS file system.
	//
	// Extra are OS paths either way: the user's own file in their home
	// directory is not part of the project.
	//
	// The bounds, MaxBytes and Budget, apply to the bytes a file
	// reads, which an FS that changes a file's text, such as one that
	// expands imports, may make larger than its Stat says.
	FS fs.FS
	// Names are the file names looked for in each directory, in order
	// of preference: the first one found is that directory's file and
	// the rest are not read, so a product can let AGENTS.override.md
	// shadow AGENTS.md, or CLAUDE.md stand in where AGENTS.md is
	// absent, as Codex reads them. Empty means [DefaultNames].
	//
	// A name is found when it equals, byte for byte, the name of an
	// entry the directory lists, whatever the host file system's case
	// rules, so agents.md is not AGENTS.md on macOS either. Each name
	// is one path element; any other is an error.
	Names []string
	// Root is the directory the walk stops after. Empty means the
	// file system root, which with FS is ".". A path outside Root is
	// an error.
	Root string
	// Extra are explicit OS paths included before the chain, in order,
	// such as the user's own file in their home directory, which the
	// repository's files then refine. Missing ones are skipped.
	//
	// They come first because the convention is that the nearest file
	// wins, and a file that is not in the tree at all is the farthest
	// of the lot: Codex reads ~/.codex/AGENTS.md first for that
	// reason. They are also the first charge on Budget, as they are
	// there.
	Extra []string
	// MaxBytes bounds one file; zero means [DefaultMaxBytes]. A file
	// that reads larger is an error, because a truncated instruction
	// file would silently change what the model is told. Chain reads
	// no more than one byte past it to find that out.
	MaxBytes int64
	// Budget bounds the total bytes of every file returned. The first
	// file that would take the total over it, and every file after
	// it, is left out and reported in [Result.Omitted], and Chain
	// returns what fits without error, so a large file deep in a tree
	// degrades the prompt rather than failing the run. No file is cut
	// short. Zero means no budget.
	//
	// The bytes charged are the bytes read. The size the file system
	// reports decides only whether a file is read: one whose reported
	// size does not fit is left out unread, and one that reads more
	// than fits is left out with the size it read.
	Budget int64
	// RenderedBudget bounds the bytes of [Render]'s output rather than
	// of the files, so a product bounding its prompt sets the number
	// the model pays: the files, the project_context element around
	// them and one project_instructions element per file, with its
	// path. It is spent as Budget is, in the same order, on the bytes
	// read, and the first file that would take the rendering over it
	// ends the chain. Zero means no rendered budget; Budget, when also
	// set, still bounds the files, and the first file over either ends
	// the chain.
	//
	// The paths counted are the ones Chain returns. A product that
	// rewrites [File.Path] before Render, such as to show a name in
	// Options.FS as a workspace path, changes the rendered size, and
	// must bound the rewritten rendering itself.
	RenderedBudget int64
}

// File is one instruction file, read verbatim.
type File struct {
	// Path is absolute, or, for a file of the chain read through
	// [Options.FS], its name in that FS. A product that shows the
	// file somewhere else maps the name to what it shows, such as a
	// workspace's root joined with it, before [Render]; that changes
	// the rendered size, which [Options.RenderedBudget] counts with the
	// path as Chain returned it.
	Path string
	// Content is the file's bytes as a string.
	Content string
}

// Result is what [Chain] found: the files that apply, in order, and
// the files it saw and left out, so a product can record or show
// exactly what the model was given and what it was not.
type Result struct {
	// Files are the instruction files to include: Options.Extra first,
	// then the chain farthest first and nearest last, so the nearest
	// file is the last text the model reads and wins.
	Files []File
	// Omitted are the files Chain found and did not include, in the
	// order it met them.
	Omitted []Omitted
}

// Omitted is one file [Chain] found and left out. A product recording
// what the model was given as parts, beside the part [Render]
// produces, names an omitted file by its Path, which is the stable key
// for the file across runs.
type Omitted struct {
	// Path is absolute, or the name in [Options.FS] as for
	// [File.Path], and is the omitted file's stable key.
	Path string
	// Size is the file's size in bytes: the bytes read, for a file
	// Chain read and left out over budget, and otherwise the size the
	// file system reported.
	Size int64
	// Reason says why the file is not in [Result.Files].
	Reason Reason
	// By is the path of the file that took this one's place, for
	// [Shadowed]; "" otherwise.
	By string
}

// Reason is why [Chain] left a file out.
type Reason int

const (
	// OverBudget: the file, or one before it, would have taken the
	// total over Options.Budget, or the rendering over
	// Options.RenderedBudget, and the chain ends at the first such
	// file.
	OverBudget Reason = iota
	// Shadowed: an earlier name in Options.Names exists in the same
	// directory and stands for it.
	Shadowed
)

// String returns "over budget" or "shadowed".
func (r Reason) String() string {
	switch r {
	case OverBudget:
		return "over budget"
	case Shadowed:
		return "shadowed"
	}
	return "reason(" + strconv.Itoa(int(r)) + ")"
}

// ErrTooLarge is wrapped in the error of a file over MaxBytes.
var ErrTooLarge = errors.New("agentsmd: file exceeds size limit")

// Chain returns the instruction files that apply at path: opts.Extra
// in order, then, in path's directory and each of its ancestors up to
// opts.Root, the first of opts.Names that the directory lists,
// farthest first and nearest last, within opts.Budget and
// opts.RenderedBudget. A file found and not included, because a
// preferred name shadows it or a budget is spent, is in
// [Result.Omitted]. A file is read only when its reported size fits
// the budgets, and the bounds apply to the bytes it reads: more than
// opts.MaxBytes is an error, and more than a budget has left omits it.
// A directory that cannot be listed is an error. path
// may be a directory, an existing file, or a file about to be
// created, whose directory is then the start. With opts.FS set, path
// is a name in it and the chain's files are named as they are in it.
func Chain(path string, opts Options) (Result, error) {
	names := opts.Names
	if len(names) == 0 {
		names = DefaultNames
	}
	for _, name := range names {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/"+string(filepath.Separator)) {
			return Result{}, fmt.Errorf("agentsmd: %q is not a valid name: a name is one path element", name)
		}
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	tree := osTree()
	if opts.FS != nil {
		tree = fsTree(opts.FS)
	}
	dirs, err := tree.dirs(path, opts.Root)
	if err != nil {
		return Result{}, err
	}
	var res Result
	var total, rendered int64
	spent := false
	// fits reports whether a file of size bytes at path fits both
	// budgets beside the files taken. newline is whether Render adds
	// one after its content, which is known only once it is read.
	fits := func(path string, size int64, newline bool) bool {
		if opts.Budget > 0 && total+size > opts.Budget {
			return false
		}
		return opts.RenderedBudget <= 0 || rendered+renderedCost(len(res.Files) == 0, path, size, newline) <= opts.RenderedBudget
	}
	// over records the file at path as omitted over budget, and the
	// chain as spent.
	over := func(path string, size int64) {
		spent = true
		res.Omitted = append(res.Omitted, Omitted{Path: path, Size: size, Reason: OverBudget})
	}
	// consider includes the file at path, opened with open, when the
	// budget allows and records it as omitted from the first file that
	// does not fit. The reported size decides whether to read it; the
	// bytes read decide the rest.
	consider := func(path string, size int64, open func(string) (io.ReadCloser, error)) error {
		// Before the read, the newline Render may add is not counted,
		// so a file is never left unread that would have fit.
		if spent || !fits(path, size, false) {
			over(path, size)
			return nil
		}
		data, err := readAtMost(open, path, maxBytes)
		if err != nil {
			return fmt.Errorf("agentsmd: %w", err)
		}
		n := int64(len(data))
		if n > maxBytes {
			return fmt.Errorf("%w: %s is more than %d bytes", ErrTooLarge, path, maxBytes)
		}
		content := string(data)
		newline := !strings.HasSuffix(content, "\n")
		if !fits(path, n, newline) {
			over(path, n)
			return nil
		}
		rendered += renderedCost(len(res.Files) == 0, path, n, newline)
		res.Files = append(res.Files, File{Path: path, Content: content})
		total += n
		return nil
	}
	for _, extra := range opts.Extra {
		abs, err := filepath.Abs(extra)
		if err != nil {
			return Result{}, fmt.Errorf("agentsmd: %w", err)
		}
		info, ok, err := stat(abs)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			continue
		}
		if err := consider(abs, info.Size(), openOS); err != nil {
			return Result{}, err
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		listed, err := tree.list(dirs[i])
		if err != nil {
			return Result{}, err
		}
		found := ""
		for _, name := range names {
			if !listed[name] {
				continue
			}
			p, err := tree.join(dirs[i], name)
			if err != nil {
				return Result{}, err
			}
			info, ok, err := tree.stat(p)
			if err != nil {
				return Result{}, err
			}
			if !ok {
				continue
			}
			if found != "" {
				res.Omitted = append(res.Omitted, Omitted{Path: p, Size: info.Size(), Reason: Shadowed, By: found})
				continue
			}
			found = p
			if err := consider(p, info.Size(), tree.open); err != nil {
				return Result{}, err
			}
		}
	}
	return res, nil
}

// tree is where the chain is walked: the OS file system, or an
// [fs.FS].
type tree struct {
	// dirs returns the directories of the chain, nearest first, from
	// the path Chain was given up to root.
	dirs func(path, root string) ([]string, error)
	// list returns the names of dir's entries, so a name is found
	// only as the directory spells it and never by a case-insensitive
	// file system answering for it. A directory that does not exist
	// lists nothing.
	list func(dir string) (map[string]bool, error)
	// join names the file called name in dir.
	join func(dir, name string) (string, error)
	// stat reports the file when it exists and is not a directory.
	stat func(name string) (fs.FileInfo, bool, error)
	// open opens the file for reading.
	open func(name string) (io.ReadCloser, error)
}

// osTree walks the OS file system with absolute paths.
func osTree() tree {
	return tree{
		dirs: osDirs,
		list: func(dir string) (map[string]bool, error) { return listed(os.ReadDir(dir)) },
		join: func(dir, name string) (string, error) { return filepath.Join(dir, name), nil },
		stat: stat,
		open: openOS,
	}
}

// osDirs returns the absolute directories from path's up to root, or
// to the file system root when root is empty.
func osDirs(path, root string) ([]string, error) {
	start, err := startDir(path)
	if err != nil {
		return nil, err
	}
	if root != "" {
		root, err = filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("agentsmd: %w", err)
		}
		if start != root && !strings.HasPrefix(start, root+string(filepath.Separator)) {
			return nil, fmt.Errorf("agentsmd: %s is outside root %s", path, root)
		}
	}
	var dirs []string
	for dir := start; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == root || filepath.Dir(dir) == dir {
			break
		}
	}
	return dirs, nil
}

// fsTree walks fsys by the names in it. Every name it asks fsys for
// is checked with [fs.ValidPath] first, so the walk never names
// anything outside fsys; whether a name inside it may lead outside,
// through a link, is fsys's to decide.
func fsTree(fsys fs.FS) tree {
	return tree{
		dirs: func(name, root string) ([]string, error) { return fsDirs(fsys, name, root) },
		list: func(dir string) (map[string]bool, error) { return listed(fs.ReadDir(fsys, dir)) },
		join: func(dir, name string) (string, error) {
			p := path.Join(dir, name)
			if !fs.ValidPath(p) {
				return "", fmt.Errorf("agentsmd: %q in %q is not a valid name in the file system", name, dir)
			}
			return p, nil
		},
		stat: func(name string) (fs.FileInfo, bool, error) { return fileInfo(fs.Stat(fsys, name)) },
		open: func(name string) (io.ReadCloser, error) { return fsys.Open(name) },
	}
}

// fsDirs returns the directories in fsys from name's up to root, or
// to "." when root is empty.
func fsDirs(fsys fs.FS, name, root string) ([]string, error) {
	if root == "" {
		root = "."
	}
	for _, n := range []string{name, root} {
		if !fs.ValidPath(n) {
			return nil, fmt.Errorf("agentsmd: %q is not a valid name in the file system", n)
		}
	}
	start := name
	info, err := fs.Stat(fsys, name)
	switch {
	case err == nil && info.IsDir():
	case err == nil || errors.Is(err, fs.ErrNotExist):
		start = path.Dir(name)
	default:
		return nil, fmt.Errorf("agentsmd: %w", err)
	}
	if root != "." && start != root && !strings.HasPrefix(start, root+"/") {
		return nil, fmt.Errorf("agentsmd: %s is outside root %s", name, root)
	}
	var dirs []string
	for dir := start; ; dir = path.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == root || dir == "." {
			break
		}
	}
	return dirs, nil
}

// openOS opens an OS file for reading.
func openOS(name string) (io.ReadCloser, error) { return os.Open(name) }

// readAtMost reads the file at name up to one byte past limit, so a
// file over the limit is known to be over without being read through:
// an FS that expands a file, or a file still being written, cannot make
// Chain read far past the bound only to refuse it.
func readAtMost(open func(string) (io.ReadCloser, error), name string, limit int64) ([]byte, error) {
	f, err := open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if limit < math.MaxInt64 {
		limit++
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

// startDir returns the absolute directory the walk starts from.
func startDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("agentsmd: %w", err)
	}
	info, err := os.Stat(abs)
	switch {
	case err == nil && info.IsDir():
		return abs, nil
	case err == nil || errors.Is(err, os.ErrNotExist):
		return filepath.Dir(abs), nil
	default:
		return "", fmt.Errorf("agentsmd: %w", err)
	}
}

// listed returns the names of a directory's entries. A directory that
// does not exist, such as that of a file about to be created in a
// directory about to be made, has none; one that exists and cannot be
// listed is an error, not an empty directory, since it may hold a file
// the chain would include.
func listed(entries []fs.DirEntry, err error) (map[string]bool, error) {
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("agentsmd: %w", err)
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names, nil
}

// stat reports the file when it exists and is not a directory. A
// missing path, or a directory of that name, is not a file.
func stat(path string) (os.FileInfo, bool, error) {
	return fileInfo(os.Stat(path))
}

// fileInfo is [stat] for the result of any stat: the file when it
// exists and is not a directory, nothing for a missing name or a
// directory, and any other error as an error.
func fileInfo(info fs.FileInfo, err error) (fs.FileInfo, bool, error) {
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("agentsmd: %w", err)
	}
	if info.IsDir() {
		return nil, false, nil
	}
	return info, true, nil
}

// PartID is the stable identifier of the one instructions part
// [Render] produces, for a product that records what the model was
// given as parts rather than as one string. The text of the part
// changes whenever the working directory moves or a file is edited;
// the identifier does not, so a reader can tell that this part moved
// and the others did not. A file considered and left out is named by
// its own [Omitted.Path].
const PartID = "agentsmd"

// Render wraps the files as pi does: one project_instructions element
// per file, with its path as the attribute, inside project_context, in
// order, so the nearest file is last and refines the rest. No files
// render as the empty string.
//
// The result is one instructions part, whose identifier is [PartID]:
// a product that hands its instructions to a session in parts hands it
// this whole string under that id, not one part per file.
func Render(files []File) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(contextOpen)
	for _, f := range files {
		b.WriteString(fileOpen)
		b.WriteString(escapeAttr(f.Path))
		b.WriteString(fileOpenEnd)
		b.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(fileClose)
	}
	b.WriteString(contextClose)
	return b.String()
}

// The pieces [Render] writes, which [renderedCost] counts.
const (
	contextOpen  = "<project_context>\n"
	contextClose = "</project_context>"
	fileOpen     = `<project_instructions path="`
	fileOpenEnd  = "\">\n"
	fileClose    = "</project_instructions>\n"
)

// renderedCost is the bytes [Render] adds to its output for one more
// file of size bytes at path: its element, its content, and the
// newline Render writes after content that does not end in one; and,
// for the first file, the project_context element around them all,
// since no files render as nothing.
func renderedCost(first bool, path string, size int64, newline bool) int64 {
	n := int64(len(fileOpen)+len(escapeAttr(path))+len(fileOpenEnd)+len(fileClose)) + size
	if newline {
		n++
	}
	if first {
		n += int64(len(contextOpen) + len(contextClose))
	}
	return n
}

var escapeAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace
