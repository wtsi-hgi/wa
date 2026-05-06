package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
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
	selectedEnv, filteredArgs := extractSelectedEnv(args)

	err := loadSelectedEnv(selectedEnv)
	if err != nil {
		return err
	}

	command := cmd.NewRootCommand()
	command.SetArgs(rewriteLegacyInspectArgs(filteredArgs))

	return command.Execute()
}

func extractSelectedEnv(args []string) (string, []string) {
	selectedEnv := canonicalEnvName(os.Getenv("WA_ENV"))
	filteredArgs := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		arg := args[index]

		if arg == "--" {
			filteredArgs = append(filteredArgs, args[index:]...)
			break
		}

		if arg == "--env" {
			if index+1 < len(args) {
				selectedEnv = canonicalEnvName(args[index+1])
				index++

				continue
			}

			filteredArgs = append(filteredArgs, arg)

			continue
		}

		if value, ok := strings.CutPrefix(arg, "--env="); ok {
			selectedEnv = canonicalEnvName(value)

			continue
		}

		filteredArgs = append(filteredArgs, arg)
	}

	return selectedEnv, filteredArgs
}

func canonicalEnvName(name string) string {
	trimmed := strings.TrimSpace(name)

	switch trimmed {
	case "dev":
		return "development"
	case "prod":
		return "production"
	default:
		return trimmed
	}
}

func loadSelectedEnv(selectedEnv string) error {
	files := envFilesFor(selectedEnv)
	if len(files) == 0 {
		return nil
	}

	existingFiles := make([]string, 0, len(files))
	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			existingFiles = append(existingFiles, file)
		}
	}

	if len(existingFiles) == 0 {
		return maybeLoadTestSagaToken(selectedEnv)
	}

	err := godotenv.Load(existingFiles...)
	if err != nil {
		return err
	}

	return maybeLoadTestSagaToken(selectedEnv)
}

func envFilesFor(selectedEnv string) []string {
	if selectedEnv == "" {
		return []string{filepath.Clean(".env")}
	}

	files := []string{
		filepath.Clean(".env." + selectedEnv + ".local"),
		filepath.Clean(".env." + selectedEnv),
	}

	if selectedEnv != "test" {
		files = append(files[:1], append([]string{filepath.Clean(".env.local")}, files[1:]...)...)
	}

	files = append(files, filepath.Clean(".env"))

	return files
}

func maybeLoadTestSagaToken(selectedEnv string) error {
	if selectedEnv != "test" || os.Getenv("SAGA_API_TOKEN") != "" {
		return nil
	}

	values, err := godotenv.Read(filepath.Clean(".env.development.local"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	if values["SAGA_API_TOKEN"] == "" {
		return nil
	}

	return os.Setenv("SAGA_API_TOKEN", values["SAGA_API_TOKEN"])
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
