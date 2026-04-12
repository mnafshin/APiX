package internal

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func modulePath(repoRoot string) (string, error) {
	f, err := os.Open(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return "", errors.New("module path not found in go.mod")
}

func TestNoImportCycles(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("could not find repo root (skipping arch test): %v", err)
		return
	}
	modPath, err := modulePath(repoRoot)
	if err != nil {
		t.Fatalf("failed to read module path: %v", err)
	}

	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedModule, Dir: repoRoot}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		t.Fatalf("packages.Load failed: %v", err)
	}

	// collect internal packages only
	prefix := modPath + "/internal/"
	nodes := map[string]*packages.Package{}
	for _, p := range pkgs {
		if strings.HasPrefix(p.PkgPath, prefix) {
			nodes[p.PkgPath] = p
		}
	}
	if len(nodes) == 0 {
		t.Skip("no internal packages found; skipping arch test")
		return
	}

	// build adjacency for internal-only imports
	graph := map[string][]string{}
	for path, p := range nodes {
		for imp := range p.Imports {
			if strings.HasPrefix(imp, prefix) {
				graph[path] = append(graph[path], imp)
			}
		}
	}

	// detect cycles via DFS
	visited := map[string]int{} // 0=unvisited,1=visiting,2=visited
	var stack []string
	var cycles [][]string
	var dfs func(string)
	dfs = func(n string) {
		if visited[n] == 1 {
			// find cycle in stack
			idx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == n {
					idx = i
					break
				}
			}
			if idx == -1 {
				return
			}
			cycle := append([]string{}, stack[idx:]...)
			cycle = append(cycle, n)
			cycles = append(cycles, cycle)
			return
		}
		if visited[n] == 2 {
			return
		}
		visited[n] = 1
		stack = append(stack, n)
		for _, m := range graph[n] {
			dfs(m)
		}
		stack = stack[:len(stack)-1]
		visited[n] = 2
	}

	for n := range nodes {
		if visited[n] == 0 {
			dfs(n)
		}
	}

	if len(cycles) > 0 {
		var b strings.Builder
		b.WriteString("import cycles detected among internal packages:\n")
		for _, c := range cycles {
			b.WriteString("  - ")
			b.WriteString(strings.Join(c, " -> "))
			b.WriteString("\n")
		}
		t.Fatalf("%s", b.String())
	}
}
