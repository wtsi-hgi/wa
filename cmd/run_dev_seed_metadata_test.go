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
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// TestRunDevSeedMetadataUsesRealSagaIdentifiers guards against regressing the
// dev fixture metadata back to fake identifiers (e.g. SANG5993, SMP5994, RNA)
// that Saga cannot resolve, which causes the seqmeta resolution dialog in
// `make dev-fixtures` / `run-dev.sh` to stall and surface "service unavailable".
func TestRunDevSeedMetadataUsesRealSagaIdentifiers(t *testing.T) {
	convey.Convey("seed.json fixtures use plausible real Saga identifiers in every seqmeta_* metadata key", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		seedPath := filepath.Join(repoRoot, ".docs", "results-web", "fixtures", "seed.json")

		raw, err := os.ReadFile(seedPath)
		convey.So(err, convey.ShouldBeNil)

		var fixtures []map[string]any

		convey.So(json.Unmarshal(raw, &fixtures), convey.ShouldBeNil)
		convey.So(len(fixtures), convey.ShouldBeGreaterThanOrEqualTo, 3)

		fakeValues := map[string]struct{}{
			"SANG5993": {},
			"SMP5994":  {},
			"RNA":      {},
		}

		digitsOnly := regexp.MustCompile(`^[0-9]+$`)

		totalSeqmetaKeys := 0

		for index, fixture := range fixtures {
			metadata, ok := fixture["metadata"].(map[string]any)
			convey.So(ok, convey.ShouldBeTrue)

			fixtureSeqmetaKeys := 0

			for key, rawValue := range metadata {
				if !strings.HasPrefix(key, "seqmeta_") {
					continue
				}

				fixtureSeqmetaKeys++
				totalSeqmetaKeys++

				value, isString := rawValue.(string)
				convey.So(isString, convey.ShouldBeTrue)
				convey.So(strings.TrimSpace(value), convey.ShouldNotBeBlank)

				_, isFake := fakeValues[value]
				convey.So(isFake, convey.ShouldBeFalse)

				switch key {
				case "seqmeta_studyid", "seqmeta_sample_lims":
					convey.So(digitsOnly.MatchString(value), convey.ShouldBeTrue)
				}
			}

			convey.So(fixtureSeqmetaKeys, convey.ShouldBeGreaterThanOrEqualTo, 1)
			_ = index
		}

		convey.So(totalSeqmetaKeys, convey.ShouldBeGreaterThanOrEqualTo, 3)
	})

	convey.Convey("seed.json fixtures demonstrate distinct single seqmeta kinds", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		seedPath := filepath.Join(repoRoot, ".docs", "results-web", "fixtures", "seed.json")

		raw, err := os.ReadFile(seedPath)
		convey.So(err, convey.ShouldBeNil)

		var fixtures []map[string]any

		convey.So(json.Unmarshal(raw, &fixtures), convey.ShouldBeNil)
		convey.So(len(fixtures), convey.ShouldBeGreaterThanOrEqualTo, 3)

		var hasStudyOnlyFixture, hasSampleOnlyFixture, hasLibraryOnlyFixture bool

		for _, fixture := range fixtures {
			metadata, ok := fixture["metadata"].(map[string]any)
			convey.So(ok, convey.ShouldBeTrue)

			seqmetaKeys := []string{}

			for key := range metadata {
				if strings.HasPrefix(key, "seqmeta_") {
					seqmetaKeys = append(seqmetaKeys, key)
				}
			}

			if len(seqmetaKeys) == 1 {
				switch seqmetaKeys[0] {
				case "seqmeta_studyid":
					hasStudyOnlyFixture = true
				case "seqmeta_sampleid":
					hasSampleOnlyFixture = true
				case "seqmeta_library":
					hasLibraryOnlyFixture = true
				}
			}
		}

		convey.So(hasStudyOnlyFixture, convey.ShouldBeTrue)
		convey.So(hasSampleOnlyFixture, convey.ShouldBeTrue)
		convey.So(hasLibraryOnlyFixture, convey.ShouldBeTrue)
	})
}
