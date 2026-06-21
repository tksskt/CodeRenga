package builtin

import (
	"context"
	"fmt"
	"github.com/tks/coderenga/internal/tools"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type fileTool struct {
	name     string
	policy   tools.Level
	modifies bool
	run      func(context.Context, tools.Request) (tools.Result, error)
}

func (t fileTool) Name() string        { return t.name }
func (t fileTool) Description() string { return t.name }
func (t fileTool) Policy() tools.Level { return t.policy }
func (t fileTool) ModifiesFiles() bool { return t.modifies }
func (t fileTool) Execute(c context.Context, r tools.Request) (tools.Result, error) {
	return t.run(c, r)
}
func Register(r *tools.Registry) error {
	for _, t := range []tools.Tool{
		fileTool{name: "builtin.read_file", policy: tools.Allow, run: read},
		fileTool{name: "builtin.write_file", policy: tools.Allow, modifies: true, run: write},
		fileTool{name: "builtin.apply_patch", policy: tools.Allow, modifies: true, run: applyPatch},
		fileTool{name: "builtin.list_files", policy: tools.Allow, run: list},
		fileTool{name: "builtin.search_text", policy: tools.Allow, run: search},
	} {
		if e := r.Register(t); e != nil {
			return e
		}
	}
	return nil
}
func arg(r tools.Request, k string) (string, error) {
	v, ok := r.Arguments[k].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("%s must be a non-empty string", k)
	}
	return v, nil
}
func safe(root, path string, mustExist bool) (string, error) {
	original := path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve cwd %q: %w", root, err)
	}
	target := filepath.Clean(path)
	var real string
	if mustExist {
		real, err = filepath.EvalSymlinks(target)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("path %q does not exist within cwd", original)
			}
			return "", fmt.Errorf("resolve path %q: %w", original, err)
		}
	} else {
		real, err = resolveWriteTarget(target)
		if err != nil {
			return "", fmt.Errorf("resolve write path %q: %w", original, err)
		}
	}
	rel, err := filepath.Rel(rootReal, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes cwd sandbox", original)
	}
	return real, nil
}

func resolveWriteTarget(target string) (string, error) {
	if _, err := os.Lstat(target); err == nil {
		return filepath.EvalSymlinks(target)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	ancestor := filepath.Dir(target)
	missing := []string{filepath.Base(target)}
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("no existing parent directory")
		}
		missing = append([]string{filepath.Base(ancestor)}, missing...)
		ancestor = parent
	}
	real, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	parts := append([]string{real}, missing...)
	return filepath.Join(parts...), nil
}

func read(_ context.Context, r tools.Request) (tools.Result, error) {
	p, e := arg(r, "path")
	if e != nil {
		return tools.Result{}, e
	}
	p, e = safe(r.Context.CWD, p, true)
	if e != nil {
		return tools.Result{}, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return tools.Result{}, e
	}
	if len(b) > 262144 {
		return tools.Result{}, fmt.Errorf("file exceeds read limit")
	}
	return tools.Result{OK: true, Content: string(b)}, nil
}
func write(_ context.Context, r tools.Request) (tools.Result, error) {
	p, e := arg(r, "path")
	if e != nil {
		return tools.Result{}, e
	}
	content, e := arg(r, "content")
	if e != nil {
		return tools.Result{}, e
	}
	p, e = safe(r.Context.CWD, p, false)
	if e != nil {
		return tools.Result{}, e
	}
	if r.Context.DryRun {
		return tools.Result{OK: true, Content: "dry-run: would write " + p}, nil
	}
	if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		return tools.Result{}, e
	}
	if e = os.WriteFile(p, []byte(content), 0644); e != nil {
		return tools.Result{}, e
	}
	return tools.Result{OK: true, Content: "wrote " + p}, nil
}

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func applyPatch(_ context.Context, r tools.Request) (tools.Result, error) {
	diff, e := arg(r, "patch")
	if e != nil {
		return tools.Result{}, e
	}
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "--- ") || !strings.HasPrefix(lines[1], "+++ ") {
		return tools.Result{}, fmt.Errorf("patch must be a unified diff")
	}
	name := strings.TrimPrefix(strings.Fields(lines[1])[1], "+++")
	name = strings.TrimPrefix(name, "b/")
	if name == "/dev/null" {
		return tools.Result{}, fmt.Errorf("file creation patches are not supported; use builtin.write_file")
	}
	path, e := safe(r.Context.CWD, name, true)
	if e != nil {
		return tools.Result{}, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return tools.Result{}, e
	}
	original := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	var output []string
	source := 0
	for i := 2; i < len(lines); {
		if lines[i] == "" {
			i++
			continue
		}
		m := hunkRE.FindStringSubmatch(lines[i])
		if m == nil {
			return tools.Result{}, fmt.Errorf("invalid hunk header %q", lines[i])
		}
		start, _ := strconv.Atoi(m[1])
		target := start - 1
		if target < source || target > len(original) {
			return tools.Result{}, fmt.Errorf("hunk starts outside source")
		}
		output = append(output, original[source:target]...)
		source = target
		i++
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") {
			line := lines[i]
			if line == "\\ No newline at end of file" {
				i++
				continue
			}
			if line == "" {
				break
			}
			switch line[0] {
			case ' ':
				if source >= len(original) || original[source] != line[1:] {
					return tools.Result{}, fmt.Errorf("context mismatch at source line %d", source+1)
				}
				output = append(output, line[1:])
				source++
			case '-':
				if source >= len(original) || original[source] != line[1:] {
					return tools.Result{}, fmt.Errorf("removal mismatch at source line %d", source+1)
				}
				source++
			case '+':
				output = append(output, line[1:])
			default:
				return tools.Result{}, fmt.Errorf("invalid hunk line")
			}
			i++
		}
	}
	output = append(output, original[source:]...)
	next := strings.Join(output, "\n")
	if r.Context.DryRun {
		return tools.Result{OK: true, Content: "dry-run: validated patch for " + path}, nil
	}
	if e = os.WriteFile(path, []byte(next), 0644); e != nil {
		return tools.Result{}, e
	}
	return tools.Result{OK: true, Content: "patched " + path}, nil
}
func list(_ context.Context, r tools.Request) (tools.Result, error) {
	p := "."
	if v, ok := r.Arguments["path"].(string); ok && v != "" {
		p = v
	}
	p, e := safe(r.Context.CWD, p, true)
	if e != nil {
		return tools.Result{}, e
	}
	var out []string
	e = filepath.WalkDir(p, func(x string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if x == p {
			return nil
		}
		rel, _ := filepath.Rel(r.Context.CWD, x)
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= 1000 {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return tools.Result{OK: e == nil, Content: strings.Join(out, "\n")}, e
}
func search(_ context.Context, r tools.Request) (tools.Result, error) {
	pattern, e := arg(r, "pattern")
	if e != nil {
		return tools.Result{}, e
	}
	re, e := regexp.Compile(pattern)
	if e != nil {
		return tools.Result{}, e
	}
	var out []string
	e = filepath.WalkDir(r.Context.CWD, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".local" {
				return fs.SkipDir
			}
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil || len(b) > 262144 {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				rel, _ := filepath.Rel(r.Context.CWD, p)
				out = append(out, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), i+1, line))
				if len(out) >= 500 {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	return tools.Result{OK: e == nil, Content: strings.Join(out, "\n")}, e
}
