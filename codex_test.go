package agentsmd

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The tree under testdata/codex is the round 2 design study's fixture
// for the README's own example: a repository with an override at its
// root and three nested directories, and one file outside the tree,
// standing for the user's own AGENTS.md in their home directory.
func codexTree(t *testing.T) (repo, global string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "codex"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, "repo"), filepath.Join(base, "global", "AGENTS.md")
}

// TestExtraComesBeforeTheChain is the README's example, which is the
// case that used to go wrong: the user's global file was appended
// after the chain, so it was the last text the model read and
// therefore the text that won, where the whole convention is that the
// nearest file wins. A developer whose own file says "always rebase"
// had it outrank every repository that says not to.
//
// Codex, which implements the convention most explicitly, reads the
// global file first for this reason: "files closer to your current
// directory override earlier guidance because they appear later in the
// combined prompt".
func TestExtraComesBeforeTheChain(t *testing.T) {
	repo, global := codexTree(t)
	deep := filepath.Join(repo, "services", "api", "handlers")

	res, err := Chain(deep, Options{
		Names: []string{"AGENTS.override.md", "AGENTS.md"},
		Root:  repo,
		Extra: []string{global},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		global,
		filepath.Join(repo, "AGENTS.override.md"),
		filepath.Join(repo, "services", "AGENTS.md"),
		filepath.Join(repo, "services", "api", "AGENTS.md"),
		filepath.Join(repo, "services", "api", "handlers", "AGENTS.md"),
	}
	if got := paths(res.Files); !reflect.DeepEqual(got, want) {
		t.Errorf("Chain().Files =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The root's committed file lost to the override beside it, and
	// says so, keyed by its own path.
	shadowed := filepath.Join(repo, "AGENTS.md")
	if len(res.Omitted) != 1 || res.Omitted[0].Path != shadowed || res.Omitted[0].Reason != Shadowed || res.Omitted[0].By != want[1] {
		t.Errorf("Chain().Omitted = %+v, want %s shadowed by the override", res.Omitted, shadowed)
	}

	// In the rendered block the user's file is first and the nearest
	// repository file is last, so the nearest text refines the rest.
	rendered := Render(res.Files)
	at := make([]int, len(want))
	for i, p := range want {
		at[i] = strings.Index(rendered, `path="`+p+`"`)
		if at[i] < 0 {
			t.Fatalf("Render() does not name %s", p)
		}
	}
	for i := 1; i < len(at); i++ {
		if at[i-1] > at[i] {
			t.Errorf("Render() writes %s after %s", want[i-1], want[i])
		}
	}
	if !strings.HasSuffix(strings.TrimSuffix(rendered, "\n</project_context>"), "</project_instructions>") {
		t.Error("Render() does not end with the last file's element")
	}
}

// TestGlobalFileIsTheFirstChargeOnTheBudget: the budget is spent from
// the first text in the prompt, so a tight budget drops the nearest
// file, as it does in Codex, rather than the user's own.
func TestGlobalFileIsTheFirstChargeOnTheBudget(t *testing.T) {
	repo, global := codexTree(t)
	deep := filepath.Join(repo, "services", "api", "handlers")
	opts := Options{Names: []string{"AGENTS.override.md", "AGENTS.md"}, Root: repo, Extra: []string{global}}

	full, err := Chain(deep, opts)
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, f := range full.Files {
		total += int64(len(f.Content))
	}
	opts.Budget = total - 1
	cut, err := Chain(deep, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(cut.Files) == 0 || cut.Files[0].Path != global {
		t.Fatalf("Chain().Files[0] = %+v, want the global file kept", paths(cut.Files))
	}
	// The shadowed root file is still reported, and the one file the
	// budget dropped is the nearest, which is the file the convention
	// says wins. Codex spends its budget the same way.
	nearest := filepath.Join(deep, "AGENTS.md")
	var overBudget []string
	for _, o := range cut.Omitted {
		if o.Reason == OverBudget {
			overBudget = append(overBudget, o.Path)
		}
	}
	if len(overBudget) != 1 || overBudget[0] != nearest {
		t.Errorf("over budget = %v, want only %s", overBudget, nearest)
	}
	if len(cut.Omitted) != 2 || cut.Omitted[0].Reason != Shadowed {
		t.Errorf("Chain().Omitted = %+v, want the shadowed root file kept alongside", cut.Omitted)
	}
}
