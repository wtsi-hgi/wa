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

package mlwhdiff

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestD2StoreSchemaHasOnlyWatermarks(t *testing.T) {
	convey.Convey("D2: store schema has no deleted cache table", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		rows, err := store.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = rows.Close() }()

		tables := []string{}
		for rows.Next() {
			var name string
			convey.So(rows.Scan(&name), convey.ShouldBeNil)
			tables = append(tables, name)
		}
		convey.So(rows.Err(), convey.ShouldBeNil)

		convey.So(tables, convey.ShouldResemble, []string{"watermarks"})
	})
}

type d2DiffSource struct {
	mlwh.Queryer

	called  []string
	studies []mlwh.Study
	samples []mlwh.Sample
	paths   []mlwh.IRODSPath
}

func (s *d2DiffSource) AllStudies(_ context.Context, limit, offset int) ([]mlwh.Study, error) {
	s.called = append(s.called, "AllStudies")
	convey.So(limit, convey.ShouldEqual, providerFetchLimit)
	convey.So(offset, convey.ShouldEqual, 0)

	return s.studies, nil
}

func (s *d2DiffSource) SamplesForStudy(_ context.Context, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	s.called = append(s.called, "SamplesForStudy")
	convey.So(studyLimsID, convey.ShouldEqual, "6568")
	convey.So(limit, convey.ShouldEqual, providerFetchLimit)
	convey.So(offset, convey.ShouldEqual, 0)

	return s.samples, nil
}

func (s *d2DiffSource) IRODSPathsForSample(_ context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
	s.called = append(s.called, "IRODSPathsForSample")
	convey.So(sangerName, convey.ShouldEqual, "S1")
	convey.So(limit, convey.ShouldEqual, providerFetchLimit)
	convey.So(offset, convey.ShouldEqual, 0)

	return s.paths, nil
}

func TestD2DiffSourceIsQueryerAndDiffOnly(t *testing.T) {
	convey.Convey("D2: DiffSource is the mlwh.Queryer alias", t, func() {
		sourceType := reflect.TypeOf((*DiffSource)(nil)).Elem()
		queryerType := reflect.TypeOf((*mlwh.Queryer)(nil)).Elem()

		convey.So(sourceType, convey.ShouldEqual, queryerType)
	})

	convey.Convey("D2: diff operations call only the required query methods", t, func() {
		ctx := context.Background()
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		source := &d2DiffSource{
			studies: []mlwh.Study{{IDStudyLims: "6568"}, {IDStudyLims: "6569"}},
			samples: []mlwh.Sample{{Name: "S1"}, {Name: "S2"}},
			paths:   []mlwh.IRODSPath{{IDProduct: "P1"}},
		}

		_, err = DiffStudies(ctx, source, store)
		convey.So(err, convey.ShouldBeNil)
		_, err = DiffStudySamples(ctx, source, store, "6568")
		convey.So(err, convey.ShouldBeNil)
		_, err = DiffSampleFiles(ctx, source, store, "S1")
		convey.So(err, convey.ShouldBeNil)

		convey.So(source.called, convey.ShouldResemble, []string{
			"AllStudies",
			"SamplesForStudy",
			"IRODSPathsForSample",
		})
	})
}

func TestD2StudyDiffWithDiffSource(t *testing.T) {
	ctx := context.Background()

	convey.Convey("D2: first study poll reports all studies as added", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		source := &d2DiffSource{studies: []mlwh.Study{
			{IDStudyLims: "6568", Name: "Alpha"},
			{IDStudyLims: "6569", Name: "Beta"},
		}}

		result, err := DiffStudies(ctx, source, store)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("D2: a changed study is reported as modified", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		source := &d2DiffSource{studies: []mlwh.Study{
			{IDStudyLims: "6568", Name: "Alpha"},
			{IDStudyLims: "6569", Name: "Beta"},
		}}
		_, err = DiffStudies(ctx, source, store)
		convey.So(err, convey.ShouldBeNil)

		source.studies = []mlwh.Study{
			{IDStudyLims: "6568", Name: "Alpha updated"},
			{IDStudyLims: "6569", Name: "Beta"},
		}
		result, err := DiffStudies(ctx, source, store)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldBeEmpty)
		convey.So(result.Modified, convey.ShouldResemble, []mlwh.Study{{IDStudyLims: "6568", Name: "Alpha updated"}})
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("D2: a missing study is removed and tombstoned", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		source := &d2DiffSource{studies: []mlwh.Study{
			{IDStudyLims: "6568", Name: "Alpha"},
			{IDStudyLims: "6569", Name: "Beta"},
		}}
		_, err = DiffStudies(ctx, source, store)
		convey.So(err, convey.ShouldBeNil)

		source.studies = []mlwh.Study{{IDStudyLims: "6569", Name: "Beta"}}
		result, err := DiffStudies(ctx, source, store)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Removed, convey.ShouldResemble, []string{"6568"})

		entries, err := store.LoadEntries("studies:all")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries["6568"].Tombstone, convey.ShouldBeTrue)
	})
}

func TestD2CurrentStateFilesAndSymbolsAreGone(t *testing.T) {
	convey.Convey("D2: only diff package sources remain", t, func() {
		dir := packageDir(t)
		needle := "en" + "rich"
		removedFiles := []string{
			needle + ".go",
			"validate.go",
			needle + "_cache_test.go",
			"server_" + needle + "_test.go",
			"validate_test.go",
		}

		for _, name := range removedFiles {
			_, err := os.Stat(filepath.Join(dir, name))
			convey.So(os.IsNotExist(err), convey.ShouldBeTrue)
		}

		offenders := filesContaining(t, dir, needle)
		convey.So(offenders, convey.ShouldBeEmpty)
	})

	convey.Convey("D2: store cache helpers are absent", t, func() {
		dir := packageDir(t)
		symbols := declaredFunctions(t, dir)

		convey.So(symbols, convey.ShouldNotContainKey, "WithEnrichTTL")
		convey.So(symbols, convey.ShouldNotContainKey, "SaveEnrichCache")
		convey.So(symbols, convey.ShouldNotContainKey, "DeleteEnrichCache")
		convey.So(symbols, convey.ShouldNotContainKey, "LoadEnrichCache")
		convey.So(symbols, convey.ShouldNotContainKey, "invalidateEnrichFor")
	})
}

func packageDir(t *testing.T) string {
	t.Helper()

	_, path, _, ok := runtime.Caller(0)
	convey.So(ok, convey.ShouldBeTrue)

	return filepath.Dir(path)
}

func filesContaining(t *testing.T, dir, needle string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	convey.So(err, convey.ShouldBeNil)

	offenders := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		convey.So(err, convey.ShouldBeNil)
		if containsString(string(body), needle) {
			offenders = append(offenders, entry.Name())
		}
	}

	return offenders
}

func containsString(haystack, needle string) bool {
	for index := range haystack {
		if len(haystack[index:]) < len(needle) {
			return false
		}
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}

	return needle == ""
}

func declaredFunctions(t *testing.T, dir string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	convey.So(err, convey.ShouldBeNil)

	fset := token.NewFileSet()
	symbols := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		convey.So(err, convey.ShouldBeNil)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok {
				symbols[fn.Name.Name] = true
			}
		}
	}

	return symbols
}
