// Package agentsmd finds and renders the instruction files of the
// AGENTS.md convention: the files that apply at a path are the ones in
// its directory and every ancestor, one per directory, and the nearest
// takes precedence.
//
// [Chain] collects them farthest first and nearest last, so that when
// a product includes every file, later text refines earlier text, and
// says which files it found and left out, so a product can record or
// show exactly what the model was given and what it was not. [Render]
// wraps the files the way pi renders project context, one
// project_instructions element per file inside project_context. The
// package imports the standard library alone.
package agentsmd

import (
	"errors"
	"fmt"
	"os"
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
	// Names are the file names looked for in each directory, in order
	// of preference: the first one found is that directory's file and
	// the rest are not read, so a product can let AGENTS.override.md
	// shadow AGENTS.md, or CLAUDE.md stand in where AGENTS.md is
	// absent, as Codex reads them. Empty means [DefaultNames].
	Names []string
	// Root is the directory the walk stops after. Empty means the
	// file system root. A path outside Root is an error.
	Root string
	// Extra are explicit paths appended after the chain, in order,
	// such as a file in the user's home directory. Missing ones are
	// skipped.
	Extra []string
	// MaxBytes bounds one file; zero means [DefaultMaxBytes]. A larger
	// file is an error, because a truncated instruction file would
	// silently change what the model is told.
	MaxBytes int64
	// Budget bounds the total bytes of every file returned. The first
	// file that would take the total over it, and every file after
	// it, is left out and reported in [Result.Omitted], and Chain
	// returns what fits without error, so a large file deep in a tree
	// degrades the prompt rather than failing the run. No file is cut
	// short. Zero means no budget.
	Budget int64
}

// File is one instruction file, read verbatim.
type File struct {
	// Path is absolute.
	Path string
	// Content is the file's bytes as a string.
	Content string
}

// Result is what [Chain] found: the files that apply, in order, and
// the files it saw and left out, so a product can record or show
// exactly what the model was given and what it was not.
type Result struct {
	// Files are the instruction files to include, farthest first and
	// nearest last, then Extra.
	Files []File
	// Omitted are the files Chain found and did not include, in the
	// order it met them.
	Omitted []Omitted
}

// Omitted is one file [Chain] found and left out.
type Omitted struct {
	// Path is absolute.
	Path string
	// Size is the file's size in bytes.
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
	// total over Options.Budget, and the chain ends at the first such
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

// Chain returns the instruction files that apply at path: in path's
// directory and each of its ancestors up to opts.Root, the first file
// of opts.Names that exists, farthest first and nearest last, followed
// by opts.Extra in order, within opts.Budget. A file found and not
// included, because a preferred name shadows it or the budget is
// spent, is in [Result.Omitted]; only a file that would be included
// is read, and only such a file over opts.MaxBytes is an error. path
// may be a directory, an existing file, or a file about to be
// created, whose directory is then the start.
func Chain(path string, opts Options) (Result, error) {
	names := opts.Names
	if len(names) == 0 {
		names = DefaultNames
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	start, err := startDir(path)
	if err != nil {
		return Result{}, err
	}
	root := ""
	if opts.Root != "" {
		root, err = filepath.Abs(opts.Root)
		if err != nil {
			return Result{}, fmt.Errorf("agentsmd: %w", err)
		}
		if start != root && !strings.HasPrefix(start, root+string(filepath.Separator)) {
			return Result{}, fmt.Errorf("agentsmd: %s is outside root %s", path, root)
		}
	}
	var dirs []string
	for dir := start; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == root || filepath.Dir(dir) == dir {
			break
		}
	}
	var res Result
	var total int64
	spent := false
	// consider includes the file at path when the budget allows and
	// records it as omitted from the first file that does not fit.
	consider := func(path string, size int64) error {
		if spent || (opts.Budget > 0 && total+size > opts.Budget) {
			spent = true
			res.Omitted = append(res.Omitted, Omitted{Path: path, Size: size, Reason: OverBudget})
			return nil
		}
		if size > maxBytes {
			return fmt.Errorf("%w: %s is %d bytes, limit %d", ErrTooLarge, path, size, maxBytes)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("agentsmd: %w", err)
		}
		res.Files = append(res.Files, File{Path: path, Content: string(data)})
		total += int64(len(data))
		return nil
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		found := ""
		for _, name := range names {
			p := filepath.Join(dirs[i], name)
			info, ok, err := stat(p)
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
			if err := consider(p, info.Size()); err != nil {
				return Result{}, err
			}
		}
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
		if err := consider(abs, info.Size()); err != nil {
			return Result{}, err
		}
	}
	return res, nil
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

// stat reports the file when it exists and is not a directory. A
// missing path, or a directory of that name, is not a file.
func stat(path string) (os.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("agentsmd: %w", err)
	}
	if info.IsDir() {
		return nil, false, nil
	}
	return info, true, nil
}

// Render wraps the files as pi does: one project_instructions element
// per file, with its path as the attribute, inside project_context, in
// order. No files render as the empty string.
func Render(files []File) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<project_context>\n")
	for _, f := range files {
		b.WriteString(`<project_instructions path="`)
		b.WriteString(escapeAttr(f.Path))
		b.WriteString("\">\n")
		b.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("</project_instructions>\n")
	}
	b.WriteString("</project_context>")
	return b.String()
}

var escapeAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace
