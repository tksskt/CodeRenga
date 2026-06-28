package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

var ErrHelp = errors.New("help requested")

type Options struct {
	CWD, ConfigPath, StateDir, Mode, Profile, Model, Instruction, InstructionFile string
	Init, ShowVersion, NoPersist, DryRun, ReadStdin, NonInteractive               bool
	MaxTurns                                                                      int
}

func Parse(args []string, out io.Writer) (Options, error) {
	var o Options
	set := flag.NewFlagSet("coderenga", flag.ContinueOnError)
	set.SetOutput(out)
	set.Usage = func() { WriteHelp(out) }
	set.StringVar(&o.CWD, "cwd", ".", "project root")
	set.StringVar(&o.ConfigPath, "config", "", "explicit config file")
	set.StringVar(&o.StateDir, "state-dir", "", "state directory")
	set.StringVar(&o.Mode, "mode", "", "agent mode")
	set.StringVar(&o.Profile, "profile", "", "LLM profile")
	set.StringVar(&o.Model, "model", "", "model override")
	set.BoolVar(&o.Init, "init", false, "initialize next to executable")
	set.BoolVar(&o.ShowVersion, "version", false, "print version")
	set.BoolVar(&o.NoPersist, "no-persist", false, "disable persistent storage")
	set.BoolVar(&o.DryRun, "dry-run", false, "do not modify project files")
	set.BoolVar(&o.NonInteractive, "non-interactive", false, "fail instead of prompting for confirmation")
	set.BoolVar(&o.ReadStdin, "stdin", false, "append stdin to instruction")
	set.IntVar(&o.MaxTurns, "max-turns", 0, "maximum model/tool loop turns")
	set.StringVar(&o.InstructionFile, "instruction-file", "", "append instruction from file")
	help := set.Bool("help", false, "print help")
	if e := set.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return o, ErrHelp
		}
		return o, e
	}
	if *help {
		WriteHelp(out)
		return o, ErrHelp
	}
	o.Instruction = strings.TrimSpace(strings.Join(set.Args(), " "))
	if o.Init && o.Instruction != "" {
		return o, fmt.Errorf("--init does not accept an instruction")
	}
	return o, nil
}
func WriteHelp(w io.Writer) {
	fmt.Fprintln(w, `CodeRenga lightweight CLI coding agent.

Usage: coderenga [options] [instruction]

  --cwd <path>       Project root
  --config <path>    Explicit config file
  --state-dir <dir>  Override state directory
  --mode <name>      Agent mode
  --profile <name>   LLM profile
  --model <name>     Model override
  --stdin            Append standard input
  --instruction-file <path> Append instruction from file
  --max-turns <n>    Maximum model/tool loop turns
  --no-persist       Use in-memory storage
  --dry-run          Do not modify project files
  --non-interactive  Fail instead of prompting for confirmation
  --init             Create coderenga.d next to executable
  --version          Print version
  --help             Print help`)
}
