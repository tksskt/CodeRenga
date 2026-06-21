package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tks/coderenga/internal/cli"
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/initfs"
	coderengaruntime "github.com/tks/coderenga/internal/runtime"
	"github.com/tks/coderenga/internal/storage"
	"github.com/tks/coderenga/internal/ui"
	templatefs "github.com/tks/coderenga/templates"
)

type Options struct{ Version, ExecutableDir string }

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, a Options) int {
	o, err := cli.Parse(args, stdout)
	if errors.Is(err, cli.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "coderenga:", err)
		return 2
	}
	if o.ShowVersion {
		fmt.Fprintf(stdout, "coderenga %s\n", a.Version)
		return 0
	}
	cwd, err := validateCWD(o.CWD)
	if err != nil {
		fmt.Fprintln(stderr, "coderenga:", err)
		return 2
	}
	bin, err := resolveExecutableDir(a.ExecutableDir)
	if err != nil {
		fmt.Fprintln(stderr, "coderenga:", err)
		return 1
	}
	if o.Init {
		if err = initfs.Initialize(bin, templatefs.Files); err != nil {
			fmt.Fprintln(stderr, "coderenga:", err)
			return 1
		}
		fmt.Fprintln(stdout, "Initialized", filepath.Join(bin, "coderenga.d"))
		return 0
	}
	if o.StateDir != "" && !o.NoPersist {
		if err = prepareStateDir(o.StateDir); err != nil {
			fmt.Fprintln(stderr, "coderenga: state-dir:", err)
			return 1
		}
	}
	instruction := o.Instruction
	if o.ReadStdin {
		b, readErr := io.ReadAll(io.LimitReader(stdin, 8<<20))
		if readErr != nil {
			fmt.Fprintln(stderr, "coderenga: stdin:", readErr)
			return 1
		}
		instruction = strings.TrimSpace(instruction + "\n" + string(b))
	}
	rt, err := coderengaruntime.New(context.Background(), coderengaruntime.Options{BinaryDir: bin, CWD: cwd, ConfigPath: o.ConfigPath, StateDir: o.StateDir, Mode: o.Mode, Profile: o.Profile, Model: o.Model, NoPersist: o.NoPersist, DryRun: o.DryRun, NonInteractive: o.NonInteractive})
	if err != nil {
		printStartupError(stderr, err)
		return 1
	}
	defer rt.Close()
	if instruction != "" {
		approvalInput := bufio.NewReader(stdin)
		rt.Approve = func(name string, _ map[string]any) bool {
			fmt.Fprintf(stdout, "Execute %s? [y/N] ", name)
			answer, readErr := approvalInput.ReadString('\n')
			if readErr != nil && len(answer) == 0 {
				return false
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			return answer == "y" || answer == "yes"
		}
		if err = rt.RunInstruction(context.Background(), instruction, stdout); err != nil {
			fmt.Fprintln(stderr, "coderenga:", err)
			return 1
		}
		return 0
	}
	return ui.RunREPL(context.Background(), stdin, stdout, rt)
}

func printStartupError(w io.Writer, err error) {
	switch {
	case errors.Is(err, config.ErrNotInitialized):
		fmt.Fprintln(w, "coderenga: configuration is not initialized.")
		fmt.Fprintln(w, `Run "coderenga --init" to create coderenga.d, then edit coderenga.d/llm.json.`)
	case errors.Is(err, config.ErrOldFormat):
		fmt.Fprintln(w, "coderenga: old configuration format detected.")
		fmt.Fprintln(w, `Please recreate configuration with "coderenga --init", or migrate config.json into llm.json, mcp.json, and tools.json.`)
	default:
		fmt.Fprintln(w, "coderenga:", err)
	}
}

func prepareStateDir(dir string) error {
	p, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	db := filepath.Join(p, "coderenga.db")
	if _, err = os.Stat(db); os.IsNotExist(err) {
		return storage.Bootstrap(db)
	}
	return err
}
func validateCWD(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	i, err := os.Stat(a)
	if err != nil {
		return "", fmt.Errorf("invalid --cwd %q: %w", p, err)
	}
	if !i.IsDir() {
		return "", fmt.Errorf("invalid --cwd %q: not a directory", p)
	}
	return filepath.Clean(a), nil
}
func resolveExecutableDir(v string) (string, error) {
	if v != "" {
		return filepath.Abs(v)
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}
