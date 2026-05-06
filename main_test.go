package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestRewriteLegacyInspectArgs(t *testing.T) {
	convey.Convey("single bare identifiers are rewritten to saga inspect", t, func() {
		convey.So(rewriteLegacyInspectArgs([]string{"6568"}), convey.ShouldResemble, []string{"saga", "inspect", "6568"})
		convey.So(rewriteLegacyInspectArgs([]string{"AM762808"}), convey.ShouldResemble, []string{"saga", "inspect", "AM762808"})
		convey.So(rewriteLegacyInspectArgs([]string{"--token", "test", "6568"}), convey.ShouldResemble, []string{"saga", "inspect", "--token", "test", "6568"})
		convey.So(rewriteLegacyInspectArgs([]string{"6568", "--token", "test"}), convey.ShouldResemble, []string{"saga", "inspect", "6568", "--token", "test"})
	})

	convey.Convey("explicit subcommands and flags are left unchanged", t, func() {
		convey.So(rewriteLegacyInspectArgs([]string{"results", "search"}), convey.ShouldResemble, []string{"results", "search"})
		convey.So(rewriteLegacyInspectArgs([]string{"--help"}), convey.ShouldResemble, []string{"--help"})
		convey.So(rewriteLegacyInspectArgs([]string{"saga"}), convey.ShouldResemble, []string{"saga"})
		convey.So(rewriteLegacyInspectArgs([]string{"delete"}), convey.ShouldResemble, []string{"delete"})
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

		err = run([]string{"--help"})

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

		err = run([]string{"--env", "test", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Getenv("WA_TEST_SENTINEL"), convey.ShouldEqual, "from-test")
	})

	convey.Convey("run in test mode can import only SAGA_API_TOKEN from .env.development.local", t, func() {
		repoRoot := t.TempDir()
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.test"), "WA_ENV=test\n")
		writeEnvFileForTest(t, filepath.Join(repoRoot, ".env.development.local"), "WA_ENV=development\nWA_DEV_RESULTS_PORT=3672\nSAGA_API_TOKEN=integration-token\n")

		cwd, err := os.Getwd()
		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Chdir(repoRoot), convey.ShouldBeNil)
		defer func() {
			convey.So(os.Chdir(cwd), convey.ShouldBeNil)
		}()

		t.Setenv("WA_ENV", "test")
		unsetEnvForTest(t, "SAGA_API_TOKEN")
		unsetEnvForTest(t, "WA_DEV_RESULTS_PORT")

		err = run([]string{"--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Getenv("SAGA_API_TOKEN"), convey.ShouldEqual, "integration-token")
		convey.So(os.Getenv("WA_DEV_RESULTS_PORT"), convey.ShouldEqual, "")
	})

	convey.Convey("run returns a flag error when --env is provided without a value", t, func() {
		err := run([]string{"--env"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "flag needs an argument: --env")
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
