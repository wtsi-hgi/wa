package main

import (
	"os"
	"strings"

	"github.com/wtsi-hgi/wa/cmd"
)

var legacyInspectValueFlags = map[string]struct{}{
	"--base-url": {},
	"--timeout":  {},
	"--token":    {},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}

func run(args []string) error {
	command := cmd.NewRootCommand()
	command.SetArgs(rewriteLegacyInspectArgs(args))

	return command.Execute()
}

func rewriteLegacyInspectArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	positionals := make([]string, 0, len(args))
	skipNext := false

	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}

		if strings.HasPrefix(arg, "--") {
			flagName := arg
			if equalsIndex := strings.Index(arg, "="); equalsIndex >= 0 {
				flagName = arg[:equalsIndex]
			}

			if _, ok := legacyInspectValueFlags[flagName]; ok && !strings.Contains(arg, "=") {
				skipNext = true
			}

			continue
		}

		if strings.HasPrefix(arg, "-") {
			continue
		}

		positionals = append(positionals, arg)
	}

	if len(positionals) != 1 {
		return args
	}

	identifier := strings.TrimSpace(positionals[0])
	if identifier == "" {
		return args
	}

	if !strings.ContainsAny(identifier, "0123456789") {
		return args
	}

	switch identifier {
	case "saga", "seqmeta", "results", "help", "completion":
		return args
	default:
		return append([]string{"saga", "inspect"}, args...)
	}
}
