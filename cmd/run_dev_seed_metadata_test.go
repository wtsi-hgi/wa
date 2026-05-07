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

// TestRunDevSeedMetadataUsesRealSeqmetaIdentifiers guards against regressing the
// dev fixture metadata back to fake identifiers (e.g. SANG5993, SMP5994, RNA)
// that seqmeta cannot resolve, which causes the seqmeta resolution dialog in
// `make dev-fixtures` / `run-dev.sh` to stall and surface "service unavailable".
func TestRunDevSeedMetadataUsesRealSeqmetaIdentifiers(t *testing.T) {
	convey.Convey("seed.json fixtures use plausible real seqmeta identifiers in every seqmeta_* metadata key", t, func() {
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

	convey.Convey("seed.json includes a galleries-demo fixture with sibling and nested eligible graphical subdirectories on disk", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		seedPath := filepath.Join(repoRoot, ".docs", "results-web", "fixtures", "seed.json")

		raw, err := os.ReadFile(seedPath)
		convey.So(err, convey.ShouldBeNil)

		var fixtures []map[string]any

		convey.So(json.Unmarshal(raw, &fixtures), convey.ShouldBeNil)

		var demo map[string]any

		for _, fixture := range fixtures {
			outputDirectory, _ := fixture["output_directory"].(string)
			if strings.HasSuffix(outputDirectory, "galleries-demo") {
				demo = fixture
				break
			}
		}

		convey.So(demo, convey.ShouldNotBeNil)

		outputDirectory := demo["output_directory"].(string)
		files := demo["files"].([]any)
		convey.So(len(files), convey.ShouldBeGreaterThanOrEqualTo, 12)

		topLevelSubdirs := make(map[string]int)
		graphicalSubdirs := make(map[string]int)
		nestedGraphicalSubdirs := make(map[string]int)
		directKinds := make(map[string]map[string]bool)
		nestedKinds := make(map[string]map[string]bool)

		for _, rawFile := range files {
			file := rawFile.(map[string]any)
			relativePath, _ := file["path"].(string)
			absolutePath := filepath.Join(repoRoot, outputDirectory, relativePath)

			info, statErr := os.Stat(absolutePath)
			convey.So(statErr, convey.ShouldBeNil)
			convey.So(info.Size(), convey.ShouldBeGreaterThan, 0)

			parts := strings.Split(relativePath, "/")
			convey.So(len(parts), convey.ShouldBeGreaterThanOrEqualTo, 2)

			topLevelSubdirs[parts[0]]++

			lower := strings.ToLower(relativePath)
			previewKind := ""

			if strings.HasSuffix(lower, ".svg") || strings.HasSuffix(lower, ".png") ||
				strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") ||
				strings.HasSuffix(lower, ".gif") || strings.HasSuffix(lower, ".webp") {
				previewKind = "image"
				graphicalSubdirs[parts[0]]++

				if len(parts) > 2 {
					nestedGraphicalSubdirs[strings.Join(parts[:len(parts)-1], "/")]++
				}
			}

			if previewKind == "" {
				switch {
				case strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".tsv"):
					previewKind = "table"
				case strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown"):
					previewKind = "markdown"
				case strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") || strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".py") || strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml"):
					previewKind = "code"
				}
			}

			if previewKind == "" {
				continue
			}

			if len(parts) == 2 {
				if directKinds[parts[0]] == nil {
					directKinds[parts[0]] = make(map[string]bool)
				}

				directKinds[parts[0]][previewKind] = true
			}

			if len(parts) > 2 {
				directoryPath := strings.Join(parts[:len(parts)-1], "/")

				if nestedKinds[directoryPath] == nil {
					nestedKinds[directoryPath] = make(map[string]bool)
				}

				nestedKinds[directoryPath][previewKind] = true
			}
		}

		convey.So(len(topLevelSubdirs), convey.ShouldBeGreaterThanOrEqualTo, 3)

		subdirsWithGraphics := 0

		for _, count := range graphicalSubdirs {
			if count > 0 {
				subdirsWithGraphics++
			}
		}

		convey.So(subdirsWithGraphics, convey.ShouldBeGreaterThanOrEqualTo, 2)

		// Each graphical subdirectory has multiple distinct files so the
		// horizontal subfolder preview gallery has visible variety on every row.
		for _, count := range graphicalSubdirs {
			if count > 0 {
				convey.So(count, convey.ShouldBeGreaterThanOrEqualTo, 2)
			}
		}

		convey.So(nestedGraphicalSubdirs["sample-a/overview"], convey.ShouldBeGreaterThanOrEqualTo, 2)
		convey.So(nestedGraphicalSubdirs["sample-a/lanes/lane-1"], convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(nestedGraphicalSubdirs["sample-a/lanes/lane-2"], convey.ShouldBeGreaterThanOrEqualTo, 1)
		convey.So(directKinds["sample-a"]["image"], convey.ShouldBeTrue)
		convey.So(directKinds["sample-a"]["table"], convey.ShouldBeTrue)
		convey.So(directKinds["sample-a"]["markdown"], convey.ShouldBeTrue)
		convey.So(directKinds["sample-a"]["code"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-1"]["image"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-1"]["table"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-1"]["markdown"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-1"]["code"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-2"]["image"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-2"]["table"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-2"]["markdown"], convey.ShouldBeTrue)
		convey.So(nestedKinds["sample-a/lanes/lane-2"]["code"], convey.ShouldBeTrue)
	})
}
