package main

import (
	"os"
	"path/filepath"
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
		convey.So(rewriteLegacyInspectArgs([]string{"seqmeta"}), convey.ShouldResemble, []string{"seqmeta"})
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

		err = run([]string{"--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(os.Getenv("WA_MLWH_DSN"), convey.ShouldEqual, "")
		convey.So(os.Getenv("WA_DEV_RESULTS_PORT"), convey.ShouldEqual, "")
	})

	convey.Convey("run returns a flag error when --env is provided without a value", t, func() {
		err := run([]string{"--env"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "flag needs an argument: --env")
	})

	convey.Convey("run rejects inherited WA_MLWH_DSN in test mode", t, func() {
		t.Setenv("WA_ENV", "test")
		t.Setenv("WA_MLWH_DSN", "mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse")

		err := run(nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_DSN")
	})

	convey.Convey("run rejects development or test-shaped WA_MLWH_PASSWORD in production mode", t, func() {
		t.Setenv("WA_ENV", "production")
		t.Setenv("WA_MLWH_PASSWORD", "mlwh_humgen_is_secure")

		err := run(nil)

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

		err := run(nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_DEV_RESULTS_HOST")
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
