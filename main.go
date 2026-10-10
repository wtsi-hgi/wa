package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/wtsi-hgi/wa/cmd"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		os.Exit(1)
	}
}

// run executes the wa command tree, writing every error to stderr exactly
// once: errors before the command runs are printed here, and command errors
// are printed by the command tree itself.
func run(args []string, stderr io.Writer) error {
	selectedEnv, filteredArgs := extractSelectedEnv(args)

	err := loadSelectedEnv(selectedEnv)
	if err == nil {
		err = cmd.ValidateScenarioEnvironment(selectedEnv)
	}

	if err != nil {
		_, _ = fmt.Fprintln(stderr, "Error:", err)

		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	command := cmd.NewRootCommand()
	command.SetContext(ctx)
	command.SetErr(stderr)
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
		return nil
	}

	err := godotenv.Load(existingFiles...)
	if err != nil {
		return err
	}

	return nil
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

func rewriteLegacyInspectArgs(args []string) []string {
	return args
}
