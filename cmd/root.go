/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	knownDevelopmentMLWHDSN      = "mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse"
	knownDevelopmentMLWHPassword = "mlwh_humgen_is_secure"
)

// NewRootCommand builds the root wa command tree.
func NewRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "wa",
		Short: "Unified wa command line tools",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	command.PersistentFlags().String("env", "", "Environment name used to load .env.<name> files before running the command")

	command.AddCommand(newSeqmetaCommand())
	command.AddCommand(newResultsCommand())
	command.AddCommand(newMLWHCommand())

	return command
}

func ValidateScenarioEnvironment(envName string) error {
	switch strings.TrimSpace(envName) {
	case "development":
		return validateDevelopmentScenarioEnvironment()
	case "test":
		return validateTestScenarioEnvironment()
	case "production":
		return validateProductionScenarioEnvironment()
	default:
		return nil
	}
}

func validateDevelopmentScenarioEnvironment() error {
	return nil
}

func validateTestScenarioEnvironment() error {
	if strings.TrimSpace(firstEnv("WA_MLWH_DSN")) != "" {
		return fmt.Errorf("WA_MLWH_DSN is not permitted when WA_ENV=test")
	}

	if strings.TrimSpace(firstEnv("WA_MLWH_PASSWORD")) != "" {
		return fmt.Errorf("WA_MLWH_PASSWORD is not permitted when WA_ENV=test")
	}

	if strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PASSWORD")) != "" {
		return fmt.Errorf("WA_MLWH_CACHE_PASSWORD is not permitted when WA_ENV=test")
	}

	cachePath := strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH"))
	if cachePath != "" && !mlwhCachePathResolvesUnderRepoTmp(cachePath) {
		return fmt.Errorf("WA_MLWH_CACHE_PATH must resolve under .tmp/ when WA_ENV=test")
	}

	return nil
}

func validateProductionScenarioEnvironment() error {
	for _, envVar := range []string{"WA_TEST_RESULTS_HOST", "WA_DEV_RESULTS_HOST"} {
		if strings.TrimSpace(firstEnv(envVar)) != "" {
			return fmt.Errorf("%s is not permitted when WA_ENV=production", envVar)
		}
	}

	if mlwhPasswordLooksNonProduction(strings.TrimSpace(firstEnv("WA_MLWH_PASSWORD"))) {
		return fmt.Errorf("WA_MLWH_PASSWORD matches a development or test literal and is not permitted when WA_ENV=production")
	}

	if mlwhPasswordLooksNonProduction(strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PASSWORD"))) {
		return fmt.Errorf("WA_MLWH_CACHE_PASSWORD matches a development or test literal and is not permitted when WA_ENV=production")
	}

	if mlwhDSNLooksNonProduction(strings.TrimSpace(firstEnv("WA_MLWH_DSN"))) {
		return fmt.Errorf("WA_MLWH_DSN looks like a development or test value and is not permitted when WA_ENV=production")
	}

	if mlwhCachePathLooksTest(strings.TrimSpace(firstEnv("WA_MLWH_CACHE_PATH"))) {
		return fmt.Errorf("WA_MLWH_CACHE_PATH looks like a test value and is not permitted when WA_ENV=production")
	}

	return nil
}

func mlwhDSNLooksNonProduction(value string) bool {
	if value == "" {
		return false
	}

	lowerValue := strings.ToLower(value)

	return value == knownDevelopmentMLWHDSN || strings.Contains(lowerValue, "localhost") || strings.Contains(lowerValue, "127.0.0.1") || strings.Contains(lowerValue, "_test")
}

func mlwhPasswordLooksNonProduction(value string) bool {
	return value == knownDevelopmentMLWHPassword
}

func mlwhCachePathLooksTest(value string) bool {
	normalized := filepath.ToSlash(value)
	return normalized == "/tmp" || strings.HasPrefix(normalized, "/tmp/") || strings.Contains(normalized, "wa-test-mlwh")
}

func mlwhCachePathResolvesUnderRepoTmp(value string) bool {
	normalized := filepath.ToSlash(value)
	return strings.HasPrefix(normalized, ".tmp/") || strings.Contains(normalized, "/.tmp/")
}
