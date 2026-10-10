package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestRewriteLegacyInspectArgs(t *testing.T) {
	convey.Convey("bare identifiers are no longer rewritten after the saga command removal", t, func() {
		convey.So(rewriteLegacyInspectArgs([]string{"6568"}), convey.ShouldResemble, []string{"6568"})
		convey.So(rewriteLegacyInspectArgs([]string{"AM762808"}), convey.ShouldResemble, []string{"AM762808"})
		convey.So(rewriteLegacyInspectArgs([]string{"--token", "test", "6568"}), convey.ShouldResemble, []string{"--token", "test", "6568"})
		convey.So(rewriteLegacyInspectArgs([]string{"6568", "--token", "test"}), convey.ShouldResemble, []string{"6568", "--token", "test"})
	})

	convey.Convey("explicit subcommands and flags are left unchanged", t, func() {
		convey.So(rewriteLegacyInspectArgs([]string{"results", "search"}), convey.ShouldResemble, []string{"results", "search"})
		convey.So(rewriteLegacyInspectArgs([]string{"--help"}), convey.ShouldResemble, []string{"--help"})
		convey.So(rewriteLegacyInspectArgs([]string{"mlwhdiff"}), convey.ShouldResemble, []string{"mlwhdiff"})
		convey.So(rewriteLegacyInspectArgs([]string{"delete"}), convey.ShouldResemble, []string{"delete"})
	})
}

func TestRunPrintsStartupErrorsOnce(t *testing.T) {
	convey.Convey("Given startup failures before and inside a command, when run executes, then each error is printed exactly once to stderr", t, func() {
		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(t.TempDir()), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		stderr := &bytes.Buffer{}

		convey.Convey("a rejected scenario environment is reported", func() {
			t.Setenv("WA_ENV", "test")
			t.Setenv("WA_MLWH_DSN", "mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse")

			err := run([]string{"mlwh", "serve"}, stderr)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(strings.Count(stderr.String(), "WA_MLWH_DSN is not permitted when WA_ENV=test"), convey.ShouldEqual, 1)
		})

		convey.Convey("an unreadable selected .env file is reported", func() {
			t.Setenv("WA_ENV", "")
			convey.So(os.Mkdir(".env", 0o700), convey.ShouldBeNil)

			err := run([]string{"--help"}, stderr)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(strings.Count(stderr.String(), err.Error()), convey.ShouldEqual, 1)
		})

		convey.Convey("mlwh serve without a cache path is rejected", func() {
			t.Setenv("WA_ENV", "")
			t.Setenv("WA_MLWH_CACHE_PATH", "")

			err := run([]string{"mlwh", "serve", "--port", "0"}, stderr)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(strings.Count(stderr.String(), "WA_MLWH_CACHE_PATH must be set"), convey.ShouldEqual, 1)
		})

		convey.Convey("mlwh sync without a DSN is reported once", func() {
			t.Setenv("WA_ENV", "")
			t.Setenv("WA_MLWH_DSN", "")

			err := run([]string{"mlwh", "sync"}, stderr)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(strings.Count(stderr.String(), "WA_MLWH_DSN must be set"), convey.ShouldEqual, 1)
		})
	})
}

func TestRunRejectsUnknownSubcommands(t *testing.T) {
	convey.Convey("Given a parent command, when run is given an unknown subcommand, then it fails with one unknown command error", t, func() {
		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(t.TempDir()), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		t.Setenv("WA_ENV", "")

		for _, parent := range []string{"mlwh", "mlwhdiff", "results"} {
			stderr := &bytes.Buffer{}

			err := run([]string{parent, "bogus"}, stderr)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(stderr.String(), convey.ShouldContainSubstring, `Error: unknown command "bogus" for "wa `+parent+`"`)
			convey.So(strings.Count(stderr.String(), "Error:"), convey.ShouldEqual, 1)
		}
	})

	convey.Convey("Given a parent command, when run is given no subcommand, then it shows help and succeeds", t, func() {
		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(t.TempDir()), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		t.Setenv("WA_ENV", "")

		for _, parent := range []string{"mlwh", "mlwhdiff", "results"} {
			stderr := &bytes.Buffer{}

			convey.So(run([]string{parent}, stderr), convey.ShouldBeNil)
			convey.So(stderr.String(), convey.ShouldBeEmpty)
		}
	})
}

func TestRunLoadsSelectedEnv(t *testing.T) {
	convey.Convey("run loads the dotenv files for the selected WA_ENV", t, func() {
		repoRoot := t.TempDir()
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.production"), "WA_TEST_SENTINEL=from-production\n")

		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(repoRoot), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		t.Setenv("WA_ENV", "production")
		unsetEnvForTest(t, "WA_TEST_SENTINEL")

		err = run([]string{"--help"}, io.Discard)

		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Getenv("WA_TEST_SENTINEL"), convey.ShouldEqual, "from-production")
	})

	convey.Convey("run lets --env override the selected WA_ENV", t, func() {
		repoRoot := t.TempDir()
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.test"), "WA_TEST_SENTINEL=from-test\n")
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.production"), "WA_TEST_SENTINEL=from-production\n")

		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(repoRoot), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		t.Setenv("WA_ENV", "production")
		unsetEnvForTest(t, "WA_TEST_SENTINEL")

		err = run([]string{"--env", "test", "--help"}, io.Discard)

		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Getenv("WA_TEST_SENTINEL"), convey.ShouldEqual, "from-test")
	})

	convey.Convey("run in test mode does not import development-local MLWH vars", t, func() {
		repoRoot := t.TempDir()
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.test"), "WA_ENV=test\n")
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.development.local"), "WA_ENV=development\nWA_DEV_RESULTS_PORT=3672\nWA_MLWH_DSN=mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse\n")

		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(repoRoot), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		t.Setenv("WA_ENV", "test")
		unsetEnvForTest(t, "WA_MLWH_DSN")
		unsetEnvForTest(t, "WA_DEV_RESULTS_PORT")

		err = run([]string{"--help"}, io.Discard)

		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Getenv("WA_MLWH_DSN"), convey.ShouldEqual, "")
		convey.So(os.Getenv("WA_DEV_RESULTS_PORT"), convey.ShouldEqual, "")
	})

	convey.Convey("run returns a flag error when --env is provided without a value", t, func() {
		err := run([]string{"--env"}, io.Discard)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "flag needs an argument: --env")
	})

	convey.Convey("run rejects inherited WA_MLWH_DSN in test mode", t, func() {
		t.Setenv("WA_ENV", "test")
		t.Setenv("WA_MLWH_DSN", "mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse")

		err := run(nil, io.Discard)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_DSN")
	})

	convey.Convey("run rejects development or test-shaped WA_MLWH_PASSWORD in production mode", t, func() {
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_MLWH_PASSWORD", "mlwh_humgen_is_secure")

		err := run(nil, io.Discard)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_PASSWORD")
	})

	convey.Convey("run rejects inherited development results host in production mode", t, func() {
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_MLWH_PASSWORD", "")
		t.Setenv("WA_MLWH_CACHE_PASSWORD", "")
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		t.Setenv("WA_DEV_RESULTS_HOST", "0.0.0.0")

		err := run(nil, io.Discard)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_DEV_RESULTS_HOST")
	})

	convey.Convey("run rejects inherited development MLWH host in production mode", t, func() {
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_MLWH_PASSWORD", "")
		t.Setenv("WA_MLWH_CACHE_PASSWORD", "")
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		t.Setenv("WA_DEV_RESULTS_HOST", "")
		t.Setenv("WA_DEV_SEQMETA_HOST", "0.0.0.0")

		err := run(nil, io.Discard)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_DEV_SEQMETA_HOST")
	})
}

func writeEnvFileForTest(t *testing.T, path string, contents string) {
	t.Helper()

	err := os.WriteFile(path, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()

	originalValue, existed := os.LookupEnv(key)
	err := os.Unsetenv(key)
	if err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}

	t.Cleanup(func() {
		if !existed {
			_ = os.Unsetenv(key)

			return
		}

		_ = os.Setenv(key, originalValue)
	})
}
