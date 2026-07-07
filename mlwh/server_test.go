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

package mlwh

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

func TestServerHandlersDoNotOwnCachesB2(t *testing.T) {
	convey.Convey("B2.8: Given server handler source, when audited, then handlers do not own cache or map state", t, func() {
		source := readPackageFile(t, "server.go")
		lowerSource := strings.ToLower(source)

		convey.So(source, convey.ShouldContainSubstring, "queryer Queryer")
		convey.So(source, convey.ShouldContainSubstring, "func mlwhEndpointHandler")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "cache")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "map[")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "mutex")
		convey.So(lowerSource, convey.ShouldNotContainSubstring, "sync.")
	})
}

type serverFakeQueryer struct {
	classifyIdentifierFunc    func(context.Context, string) (Match, error)
	resolveStudyFunc          func(context.Context, string) (Match, error)
	samplesForStudyFunc       func(context.Context, string, int, int) ([]Sample, error)
	studyManifestFunc         func(context.Context, string, string, bool, int, int) (StudyManifest, error)
	enrichFunc                func(context.Context, string) (EnrichmentResult, error)
	expandIdentifierFunc      func(context.Context, IdentifierKind, string) ([]TaggedID, error)
	searchStudiesFunc         func(context.Context, string, int, int) ([]Study, error)
	searchSamplesFunc         func(context.Context, string, int, int) ([]Sample, error)
	searchSamplesWithOptsFunc func(context.Context, string, SampleSearchOptions, int, int) ([]Sample, error)
	studyUsersFunc            func(context.Context, string, string, int, int) ([]StudyUser, error)
	sampleCRAMsFunc           func(context.Context, string, int, int) ([]SampleCRAM, error)
	countStudySearchFunc      func(context.Context, string) (Count, error)
	countSampleSearchFunc     func(context.Context, string) (Count, error)
	countSampleWithOptsFunc   func(context.Context, string, SampleSearchOptions) (Count, error)
	countStudiesFunc          func(context.Context) (Count, error)
	countStudyManifestFunc    func(context.Context, string) (Count, error)
	countSamplesForStudyFunc  func(context.Context, string) (Count, error)
	countSamplesWithDataFunc  func(context.Context, string) (Count, error)
	countLatestDataStudyFunc  func(context.Context, string, string) (Count, error)
	countLatestDataPINameFunc func(context.Context, string, string) (Count, error)
	countStudyUsersFunc       func(context.Context, string, string) (Count, error)
	countSampleCRAMsFunc      func(context.Context, string) (Count, error)
	irodsStudyWithOptionsFunc func(context.Context, string, IRODSPathOptions, int, int) ([]IRODSPath, error)
	countIRODSStudyOptsFunc   func(context.Context, string, IRODSPathOptions) (Count, error)
	freshnessFunc             func(context.Context) (Freshness, error)

	samplesForStudyCall struct {
		studyLimsID string
		limit       int
		offset      int
	}

	searchCall struct {
		term   string
		limit  int
		offset int
	}

	searchOptionsCall struct {
		term   string
		opts   SampleSearchOptions
		limit  int
		offset int
	}

	irodsOptionsCall struct {
		studyLimsID string
		opts        IRODSPathOptions
		limit       int
		offset      int
	}

	studyManifestCall struct {
		studyLimsID string
		fileType    string
		withIRODS   bool
		limit       int
		offset      int
	}

	latestDataCall struct {
		studyLimsID string
		name        string
		fileType    string
		limit       int
		offset      int
	}

	studyUsersCall struct {
		studyLimsID string
		role        string
		limit       int
		offset      int
	}

	sampleCRAMsCall struct {
		studyLimsID string
		limit       int
		offset      int
	}

	countCall struct {
		term        string
		studyLimsID string
		role        string
	}
}

func (q *serverFakeQueryer) ClassifyIdentifier(ctx context.Context, raw string) (Match, error) {
	return q.classifyIdentifierFunc(ctx, raw)
}

func (q *serverFakeQueryer) ResolveSample(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveSample call")
}

func (q *serverFakeQueryer) ResolveSampleName(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveSampleName call")
}

func (q *serverFakeQueryer) ResolveStudy(ctx context.Context, raw string) (Match, error) {
	return q.resolveStudyFunc(ctx, raw)
}

func (q *serverFakeQueryer) ResolveRun(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveRun call")
}

func (q *serverFakeQueryer) ResolveLibrary(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveLibrary call")
}

func (q *serverFakeQueryer) ResolveLibraryIdentifier(_ context.Context, _ string) (Match, error) {
	panic("unexpected ResolveLibraryIdentifier call")
}

func (q *serverFakeQueryer) AllStudies(_ context.Context, _ int, _ int) ([]Study, error) {
	panic("unexpected AllStudies call")
}

func (q *serverFakeQueryer) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Sample, error) {
	q.samplesForStudyCall.studyLimsID = studyLimsID
	q.samplesForStudyCall.limit = limit
	q.samplesForStudyCall.offset = offset

	return q.samplesForStudyFunc(ctx, studyLimsID, limit, offset)
}

func (q *serverFakeQueryer) SamplesForRun(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForRun call")
}

func (q *serverFakeQueryer) SamplesForLibrary(_ context.Context, _ string, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibrary call")
}

func (q *serverFakeQueryer) SamplesForLibraryID(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibraryID call")
}

func (q *serverFakeQueryer) SamplesForLibraryLimsID(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibraryLimsID call")
}

func (q *serverFakeQueryer) SamplesForLibraryType(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
	panic("unexpected SamplesForLibraryType call")
}

func (q *serverFakeQueryer) LibrariesForStudy(_ context.Context, _ string, _ int, _ int) ([]Library, error) {
	panic("unexpected LibrariesForStudy call")
}

func (q *serverFakeQueryer) RunsForStudy(_ context.Context, _ string, _ int, _ int) ([]Run, error) {
	panic("unexpected RunsForStudy call")
}

func (q *serverFakeQueryer) RunsForSample(_ context.Context, _ string, _ int, _ int) ([]Run, error) {
	panic("unexpected RunsForSample call")
}

func (q *serverFakeQueryer) MonthlyRunCounts(_ context.Context, _ RunAggregationOptions) ([]MonthlyRunCount, error) {
	panic("unexpected MonthlyRunCounts call")
}

func (q *serverFakeQueryer) RunListing(_ context.Context, _ RunAggregationOptions, _ int, _ string) ([]RunListingRow, error) {
	panic("unexpected RunListing call")
}

func (q *serverFakeQueryer) SequencingAggregate(_ context.Context, _ SequencingAggregateOptions) ([]SequencingAggregateRow, error) {
	panic("unexpected SequencingAggregate call")
}

func (q *serverFakeQueryer) StudyOverview(_ context.Context, _ string) (StudyOverview, error) {
	panic("unexpected StudyOverview call")
}

func (q *serverFakeQueryer) RunOverview(_ context.Context, _ string) (RunOverview, error) {
	panic("unexpected RunOverview call")
}

func (q *serverFakeQueryer) RunStatus(_ context.Context, _ string) (RunStatusTimeline, error) {
	panic("unexpected RunStatus call")
}

func (q *serverFakeQueryer) SampleProgress(_ context.Context, _ string) (SampleProgress, error) {
	panic("unexpected SampleProgress call")
}

func (q *serverFakeQueryer) StatusBreakdown(_ context.Context, _ string) (StatusBreakdown, error) {
	panic("unexpected StatusBreakdown call")
}

func (q *serverFakeQueryer) SamplesWithData(_ context.Context, _ string, _ int, _ int) ([]SampleWithData, error) {
	panic("unexpected SamplesWithData call")
}

func (q *serverFakeQueryer) SamplesWithoutData(_ context.Context, _ string, _ int, _ int) ([]SampleWithData, error) {
	panic("unexpected SamplesWithoutData call")
}

func (q *serverFakeQueryer) LatestDataForStudy(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]RecentDataRow, error) {
	q.latestDataCall.studyLimsID = studyLimsID
	q.latestDataCall.fileType = fileType
	q.latestDataCall.limit = limit
	q.latestDataCall.offset = offset

	return []RecentDataRow{}, nil
}

func (q *serverFakeQueryer) LatestDataForFacultySponsor(ctx context.Context, name, fileType string, limit, offset int) ([]RecentDataRow, error) {
	q.latestDataCall.name = name
	q.latestDataCall.fileType = fileType
	q.latestDataCall.limit = limit
	q.latestDataCall.offset = offset

	return []RecentDataRow{}, nil
}

func (q *serverFakeQueryer) LanesForSample(_ context.Context, _ string, _ int, _ int) ([]Lane, error) {
	panic("unexpected LanesForSample call")
}

func (q *serverFakeQueryer) IRODSPathsForSample(_ context.Context, _ string, _ int, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForSample call")
}

func (q *serverFakeQueryer) IRODSPathsForStudy(_ context.Context, _ string, _ int, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForStudy call")
}

func (q *serverFakeQueryer) IRODSPathsForRun(_ context.Context, _ string, _ string, _ int, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForRun call")
}

func (q *serverFakeQueryer) StudyManifest(ctx context.Context, studyLimsID, fileType string, withIRODS bool, limit, offset int) (StudyManifest, error) {
	if q.studyManifestFunc == nil {
		panic("unexpected StudyManifest call")
	}

	q.studyManifestCall.studyLimsID = studyLimsID
	q.studyManifestCall.fileType = fileType
	q.studyManifestCall.withIRODS = withIRODS
	q.studyManifestCall.limit = limit
	q.studyManifestCall.offset = offset

	return q.studyManifestFunc(ctx, studyLimsID, fileType, withIRODS, limit, offset)
}

func (q *serverFakeQueryer) StudiesForSample(_ context.Context, _ string) ([]Study, error) {
	panic("unexpected StudiesForSample call")
}

func (q *serverFakeQueryer) StudiesForProgramme(_ context.Context, _ string, _ int, _ int) ([]Study, error) {
	panic("unexpected StudiesForProgramme call")
}

func (q *serverFakeQueryer) StudyUsers(ctx context.Context, studyLimsID, role string, limit, offset int) ([]StudyUser, error) {
	if q.studyUsersFunc == nil {
		panic("unexpected StudyUsers call")
	}

	q.studyUsersCall.studyLimsID = studyLimsID
	q.studyUsersCall.role = role
	q.studyUsersCall.limit = limit
	q.studyUsersCall.offset = offset

	return q.studyUsersFunc(ctx, studyLimsID, role, limit, offset)
}

func (q *serverFakeQueryer) SampleCRAMsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]SampleCRAM, error) {
	if q.sampleCRAMsFunc == nil {
		panic("unexpected SampleCRAMsForStudy call")
	}

	q.sampleCRAMsCall.studyLimsID = studyLimsID
	q.sampleCRAMsCall.limit = limit
	q.sampleCRAMsCall.offset = offset

	return q.sampleCRAMsFunc(ctx, studyLimsID, limit, offset)
}

func (q *serverFakeQueryer) Export(_ context.Context, _ ExportRelationship, _ string, _ ExportOptions) (ExportResult, error) {
	panic("unexpected Export call")
}

func (q *serverFakeQueryer) StudiesForFacultySponsor(_ context.Context, _ string, _ int, _ int) ([]PersonStudy, error) {
	panic("unexpected StudiesForFacultySponsor call")
}

func (q *serverFakeQueryer) CountStudiesForFacultySponsor(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountStudiesForFacultySponsor call")
}

func (q *serverFakeQueryer) StudiesForUser(_ context.Context, _, _ string, _ int, _ int) ([]PersonStudy, error) {
	panic("unexpected StudiesForUser call")
}

func (q *serverFakeQueryer) CountStudiesForUser(_ context.Context, _, _ string) (Count, error) {
	panic("unexpected CountStudiesForUser call")
}

func (q *serverFakeQueryer) ResolvePerson(_ context.Context, _ string, _ int, _ int) ([]PersonCandidate, error) {
	panic("unexpected ResolvePerson call")
}

func (q *serverFakeQueryer) CountResolvePerson(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountResolvePerson call")
}

func (q *serverFakeQueryer) FindSamplesBySangerID(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesBySangerID call")
}

func (q *serverFakeQueryer) FindSamplesByIDSampleLims(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesByIDSampleLims call")
}

func (q *serverFakeQueryer) FindSamplesByAccessionNumber(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesByAccessionNumber call")
}

func (q *serverFakeQueryer) FindSamplesBySupplierName(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesBySupplierName call")
}

func (q *serverFakeQueryer) FindSamplesByLibraryType(_ context.Context, _ string) ([]Sample, error) {
	panic("unexpected FindSamplesByLibraryType call")
}

func (q *serverFakeQueryer) ExpandIdentifier(ctx context.Context, kind IdentifierKind, raw string) ([]TaggedID, error) {
	if q.expandIdentifierFunc == nil {
		panic("unexpected ExpandIdentifier call")
	}

	return q.expandIdentifierFunc(ctx, kind, raw)
}

func (q *serverFakeQueryer) SearchStudies(ctx context.Context, term string, limit, offset int) ([]Study, error) {
	if q.searchStudiesFunc == nil {
		panic("unexpected SearchStudies call")
	}

	q.searchCall.term = term
	q.searchCall.limit = limit
	q.searchCall.offset = offset

	return q.searchStudiesFunc(ctx, term, limit, offset)
}

func (q *serverFakeQueryer) SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	if q.searchSamplesFunc == nil {
		panic("unexpected SearchSamples call")
	}

	q.searchCall.term = term
	q.searchCall.limit = limit
	q.searchCall.offset = offset

	return q.searchSamplesFunc(ctx, term, limit, offset)
}

func (q *serverFakeQueryer) SearchSamplesWithOptions(ctx context.Context, term string, opts SampleSearchOptions, limit, offset int) ([]Sample, error) {
	if q.searchSamplesWithOptsFunc == nil {
		panic("unexpected SearchSamplesWithOptions call")
	}

	q.searchOptionsCall.term = term
	q.searchOptionsCall.opts = opts
	q.searchOptionsCall.limit = limit
	q.searchOptionsCall.offset = offset

	return q.searchSamplesWithOptsFunc(ctx, term, opts, limit, offset)
}

// The Count stubs below back the X-Total-Count header path of their companion
// paginated list (SearchStudies/SearchSamples/AllStudies/SamplesForStudy and the
// samples-with/without-data lists), so a list-only test now reaches them through
// the sizing-header resolution. They therefore return a zero Count when no Func
// is set (the list assertions cover the body and pagination, not the count)
// rather than panicking, and run the supplied Func when a test exercises the
// count explicitly.
func (q *serverFakeQueryer) CountStudySearch(ctx context.Context, term string) (Count, error) {
	q.countCall.term = term

	if q.countStudySearchFunc == nil {
		return Count{}, nil
	}

	return q.countStudySearchFunc(ctx, term)
}

func (q *serverFakeQueryer) CountSampleSearch(ctx context.Context, term string) (Count, error) {
	q.countCall.term = term

	if q.countSampleSearchFunc == nil {
		return Count{}, nil
	}

	return q.countSampleSearchFunc(ctx, term)
}

func (q *serverFakeQueryer) CountSampleSearchWithOptions(ctx context.Context, term string, opts SampleSearchOptions) (Count, error) {
	q.searchOptionsCall.term = term
	q.searchOptionsCall.opts = opts

	if q.countSampleWithOptsFunc == nil {
		return Count{}, nil
	}

	return q.countSampleWithOptsFunc(ctx, term, opts)
}

func (q *serverFakeQueryer) CountStudies(ctx context.Context) (Count, error) {
	if q.countStudiesFunc == nil {
		return Count{}, nil
	}

	return q.countStudiesFunc(ctx)
}

func (q *serverFakeQueryer) CountSamplesForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	q.countCall.studyLimsID = studyLimsID

	if q.countSamplesForStudyFunc == nil {
		return Count{}, nil
	}

	return q.countSamplesForStudyFunc(ctx, studyLimsID)
}

func (q *serverFakeQueryer) CountSamplesWithData(ctx context.Context, studyLimsID string) (Count, error) {
	q.countCall.studyLimsID = studyLimsID

	if q.countSamplesWithDataFunc == nil {
		return Count{}, nil
	}

	return q.countSamplesWithDataFunc(ctx, studyLimsID)
}

func (q *serverFakeQueryer) CountLatestDataForStudy(ctx context.Context, studyLimsID, fileType string) (Count, error) {
	q.latestDataCall.studyLimsID = studyLimsID
	q.latestDataCall.fileType = fileType

	if q.countLatestDataStudyFunc == nil {
		return Count{}, nil
	}

	return q.countLatestDataStudyFunc(ctx, studyLimsID, fileType)
}

func (q *serverFakeQueryer) CountLatestDataForFacultySponsor(ctx context.Context, name, fileType string) (Count, error) {
	q.latestDataCall.name = name
	q.latestDataCall.fileType = fileType

	if q.countLatestDataPINameFunc == nil {
		return Count{}, nil
	}

	return q.countLatestDataPINameFunc(ctx, name, fileType)
}

func (q *serverFakeQueryer) Freshness(ctx context.Context) (Freshness, error) {
	if q.freshnessFunc == nil {
		panic("unexpected Freshness call")
	}

	return q.freshnessFunc(ctx)
}

func (q *serverFakeQueryer) ExpandSearchValues(_ context.Context, _ IdentifierKind, _ string) (SearchValues, error) {
	panic("unexpected ExpandSearchValues call")
}

func (q *serverFakeQueryer) ExpandSampleSearchValues(_ context.Context, _ IdentifierKind, _ string) ([]string, error) {
	panic("unexpected ExpandSampleSearchValues call")
}

func (q *serverFakeQueryer) Enrich(ctx context.Context, identifier string) (EnrichmentResult, error) {
	return q.enrichFunc(ctx, identifier)
}

func (q *serverFakeQueryer) SampleDetail(_ context.Context, _ string) (SampleDetail, error) {
	panic("unexpected SampleDetail call")
}

func (q *serverFakeQueryer) StudyDetail(_ context.Context, _ string) (StudyDetail, error) {
	panic("unexpected StudyDetail call")
}

func (q *serverFakeQueryer) RunDetail(_ context.Context, _ string) (RunDetail, error) {
	panic("unexpected RunDetail call")
}

func (q *serverFakeQueryer) LibraryDetail(_ context.Context, _ string, _ string) (LibraryDetail, error) {
	panic("unexpected LibraryDetail call")
}

func (q *serverFakeQueryer) CountSamplesForRun(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountSamplesForRun call")
}

func (q *serverFakeQueryer) CountSamplesForLibrary(_ context.Context, _ string, _ string) (Count, error) {
	panic("unexpected CountSamplesForLibrary call")
}

func (q *serverFakeQueryer) CountSamplesForLibraryID(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountSamplesForLibraryID call")
}

func (q *serverFakeQueryer) CountSamplesForLibraryLimsID(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountSamplesForLibraryLimsID call")
}

func (q *serverFakeQueryer) CountSamplesForLibraryType(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountSamplesForLibraryType call")
}

func (q *serverFakeQueryer) CountRunsForStudy(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountRunsForStudy call")
}

func (q *serverFakeQueryer) CountRunsForSample(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountRunsForSample call")
}

func (q *serverFakeQueryer) CountRunListing(_ context.Context, _ RunAggregationOptions) (Count, error) {
	panic("unexpected CountRunListing call")
}

func (q *serverFakeQueryer) CountStudiesForSample(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountStudiesForSample call")
}

func (q *serverFakeQueryer) CountStudiesForProgramme(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountStudiesForProgramme call")
}

func (q *serverFakeQueryer) Programmes(_ context.Context) ([]Programme, error) {
	panic("unexpected Programmes call")
}

func (q *serverFakeQueryer) CountStudyManifest(ctx context.Context, studyLimsID string) (Count, error) {
	q.countCall.studyLimsID = studyLimsID

	if q.countStudyManifestFunc == nil {
		return Count{}, nil
	}

	return q.countStudyManifestFunc(ctx, studyLimsID)
}

func (q *serverFakeQueryer) CountLibrariesForStudy(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountLibrariesForStudy call")
}

func (q *serverFakeQueryer) CountLanesForSample(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountLanesForSample call")
}

func (q *serverFakeQueryer) CountStudyUsers(ctx context.Context, studyLimsID, role string) (Count, error) {
	q.countCall.studyLimsID = studyLimsID
	q.countCall.role = role

	if q.countStudyUsersFunc == nil {
		return Count{}, nil
	}

	return q.countStudyUsersFunc(ctx, studyLimsID, role)
}

func (q *serverFakeQueryer) CountSampleCRAMsForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	q.countCall.studyLimsID = studyLimsID

	if q.countSampleCRAMsFunc == nil {
		return Count{}, nil
	}

	return q.countSampleCRAMsFunc(ctx, studyLimsID)
}

func (q *serverFakeQueryer) CountIRODSPathsForSample(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountIRODSPathsForSample call")
}

func (q *serverFakeQueryer) CountIRODSPathsForStudy(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountIRODSPathsForStudy call")
}

func (q *serverFakeQueryer) CountIRODSPathsForRun(_ context.Context, _ string, _ string) (Count, error) {
	panic("unexpected CountIRODSPathsForRun call")
}

func (q *serverFakeQueryer) IRODSPathsForStudyWithOptions(ctx context.Context, studyLimsID string, opts IRODSPathOptions, limit, offset int) ([]IRODSPath, error) {
	if q.irodsStudyWithOptionsFunc == nil {
		panic("unexpected IRODSPathsForStudyWithOptions call")
	}

	q.irodsOptionsCall.studyLimsID = studyLimsID
	q.irodsOptionsCall.opts = opts
	q.irodsOptionsCall.limit = limit
	q.irodsOptionsCall.offset = offset

	return q.irodsStudyWithOptionsFunc(ctx, studyLimsID, opts, limit, offset)
}

func (q *serverFakeQueryer) IRODSPathsForSampleWithOptions(_ context.Context, _ string, _ IRODSPathOptions, _, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForSampleWithOptions call")
}

func (q *serverFakeQueryer) IRODSPathsForRunWithOptions(_ context.Context, _ string, _ IRODSPathOptions, _, _ int) ([]IRODSPath, error) {
	panic("unexpected IRODSPathsForRunWithOptions call")
}

func (q *serverFakeQueryer) CountIRODSPathsForStudyWithOptions(ctx context.Context, studyLimsID string, opts IRODSPathOptions) (Count, error) {
	q.irodsOptionsCall.studyLimsID = studyLimsID
	q.irodsOptionsCall.opts = opts

	if q.countIRODSStudyOptsFunc == nil {
		return Count{}, nil
	}

	return q.countIRODSStudyOptsFunc(ctx, studyLimsID, opts)
}

func (q *serverFakeQueryer) CountIRODSPathsForSampleWithOptions(_ context.Context, _ string, _ IRODSPathOptions) (Count, error) {
	panic("unexpected CountIRODSPathsForSampleWithOptions call")
}

func (q *serverFakeQueryer) CountIRODSPathsForRunWithOptions(_ context.Context, _ string, _ IRODSPathOptions) (Count, error) {
	panic("unexpected CountIRODSPathsForRunWithOptions call")
}

func (q *serverFakeQueryer) CountFindSamplesBySangerID(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountFindSamplesBySangerID call")
}

func (q *serverFakeQueryer) CountFindSamplesByIDSampleLims(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountFindSamplesByIDSampleLims call")
}

func (q *serverFakeQueryer) CountFindSamplesByAccessionNumber(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountFindSamplesByAccessionNumber call")
}

func (q *serverFakeQueryer) CountFindSamplesBySupplierName(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountFindSamplesBySupplierName call")
}

func (q *serverFakeQueryer) CountFindSamplesByLibraryType(_ context.Context, _ string) (Count, error) {
	panic("unexpected CountFindSamplesByLibraryType call")
}

func TestServerSamplesForStudyB2(t *testing.T) {
	convey.Convey("B2.1: Given a server over a fake Queryer, when GET /study/6568/samples is served, then status is 200 and body is a 2-element Sample array", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, studyLimsID string, limit, offset int) ([]Sample, error) {
				return []Sample{
					{IDSampleTmp: 1, Name: studyLimsID + "-A"},
					{IDSampleTmp: 2, Name: studyLimsID + "-B"},
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.samplesForStudyCall.studyLimsID, convey.ShouldEqual, "6568")
		convey.So(queryer.samplesForStudyCall.limit, convey.ShouldEqual, mlwhServerFetchAllLimit)
		convey.So(queryer.samplesForStudyCall.offset, convey.ShouldEqual, 0)

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)
		convey.So(samples, convey.ShouldResemble, []Sample{
			{IDSampleTmp: 1, Name: "6568-A"},
			{IDSampleTmp: 2, Name: "6568-B"},
		})
	})

	convey.Convey("B2.2: Given limit and offset query params, when GET /study/6568/samples is served, then the queryer receives those values", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
				return []Sample{}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples?limit=2&offset=1")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.samplesForStudyCall.limit, convey.ShouldEqual, 2)
		convey.So(queryer.samplesForStudyCall.offset, convey.ShouldEqual, 1)
	})
}

func performMLWHRequestForTest(t *testing.T, queryer Queryer, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewServer(queryer).RegisterRoutes(router, nil)

	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func decodeMLWHJSONResponseForTest(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func TestServerD1cStudyUsersSizingHeaders(t *testing.T) {
	convey.Convey("D1c: Given a server over a fake Queryer for study users", t, func() {
		queryer := &serverFakeQueryer{
			studyUsersFunc: func(_ context.Context, _ string, _ string, _ int, _ int) ([]StudyUser, error) {
				return []StudyUser{{Role: "owner", Name: "Ana Owner", Login: "ao1", Email: "ao1@sanger.ac.uk"}}, nil
			},
			countStudyUsersFunc: func(_ context.Context, _ string, _ string) (Count, error) {
				return Count{Count: 2}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/E1/users?role=owner&limit=1&offset=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "2")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "1")
		convey.So(queryer.studyUsersCall.studyLimsID, convey.ShouldEqual, "E1")
		convey.So(queryer.studyUsersCall.role, convey.ShouldEqual, "owner")
		convey.So(queryer.studyUsersCall.limit, convey.ShouldEqual, 1)
		convey.So(queryer.studyUsersCall.offset, convey.ShouldEqual, 0)
		convey.So(queryer.countCall.role, convey.ShouldEqual, "owner")

		var users []StudyUser
		decodeMLWHJSONResponseForTest(t, response, &users)
		convey.So(users, convey.ShouldResemble, []StudyUser{{Role: "owner", Name: "Ana Owner", Login: "ao1", Email: "ao1@sanger.ac.uk"}})
	})
}

func TestServerSampleCRAMsForStudySizingHeadersH3(t *testing.T) {
	convey.Convey("H3: Given a server over a fake Queryer for sample-crams", t, func() {
		queryer := &serverFakeQueryer{
			sampleCRAMsFunc: func(_ context.Context, _ string, _ int, _ int) ([]SampleCRAM, error) {
				return []SampleCRAM{{Name: "7568STDY9419243", EGAID: "ERS7568001", IRODSCRAMPath: "/seq/49348_1-2#1.cram", Merged: true}}, nil
			},
			countSampleCRAMsFunc: func(_ context.Context, _ string) (Count, error) {
				return Count{Count: 732}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/7568/sample-crams?limit=1&offset=48")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "732")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "49")
		convey.So(queryer.sampleCRAMsCall.studyLimsID, convey.ShouldEqual, "7568")
		convey.So(queryer.sampleCRAMsCall.limit, convey.ShouldEqual, 1)
		convey.So(queryer.sampleCRAMsCall.offset, convey.ShouldEqual, 48)
		convey.So(queryer.countCall.studyLimsID, convey.ShouldEqual, "7568")

		var rows []SampleCRAM
		decodeMLWHJSONResponseForTest(t, response, &rows)
		convey.So(rows, convey.ShouldResemble, []SampleCRAM{{
			Name:          "7568STDY9419243",
			EGAID:         "ERS7568001",
			IRODSCRAMPath: "/seq/49348_1-2#1.cram",
			Merged:        true,
		}})
	})
}

func TestServerSearchSamplesForwardsOptionsJ(t *testing.T) {
	convey.Convey("J: Given a server over a fake Queryer for optioned sample search", t, func() {
		queryer := &serverFakeQueryer{
			searchSamplesWithOptsFunc: func(_ context.Context, _ string, _ SampleSearchOptions, _, _ int) ([]Sample, error) {
				return []Sample{{Name: "Hek_R1"}}, nil
			},
			countSampleWithOptsFunc: func(_ context.Context, _ string, _ SampleSearchOptions) (Count, error) {
				return Count{Count: 1}, nil
			},
		}

		response := performMLWHRequestForTest(
			t,
			queryer,
			http.MethodGet,
			"/search/sample/hek_r?words=true&organism=musculus&library_type=Standard&qc=pass&deliverables_only=true&limit=1&offset=2",
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "1")
		convey.So(queryer.searchOptionsCall.term, convey.ShouldEqual, "hek_r")
		convey.So(queryer.searchOptionsCall.limit, convey.ShouldEqual, 1)
		convey.So(queryer.searchOptionsCall.offset, convey.ShouldEqual, 2)
		convey.So(queryer.searchOptionsCall.opts, convey.ShouldResemble, SampleSearchOptions{
			Words:            true,
			Organism:         "musculus",
			LibraryType:      "Standard",
			QC:               "pass",
			DeliverablesOnly: true,
		})
	})
}

func TestServerIRODSPathsForStudyForwardsDeliverablesOnlyJ(t *testing.T) {
	convey.Convey("J: Given a server over a fake Queryer for optioned study iRODS paths", t, func() {
		queryer := &serverFakeQueryer{
			irodsStudyWithOptionsFunc: func(_ context.Context, _ string, _ IRODSPathOptions, _, _ int) ([]IRODSPath, error) {
				return []IRODSPath{{IRODSPath: "/seq/S1.cram", ManualQC: "pass"}}, nil
			},
			countIRODSStudyOptsFunc: func(_ context.Context, _ string, _ IRODSPathOptions) (Count, error) {
				return Count{Count: 1}, nil
			},
		}

		response := performMLWHRequestForTest(
			t,
			queryer,
			http.MethodGet,
			"/study/S1/irods?file_type=cram&deliverables_only=true&order_by=created_desc&since=2026-07-01T00:00:00Z&until=2026-07-02T00:00:00Z&limit=1&offset=0",
		)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "1")
		convey.So(queryer.irodsOptionsCall.studyLimsID, convey.ShouldEqual, "S1")
		convey.So(queryer.irodsOptionsCall.limit, convey.ShouldEqual, 1)
		convey.So(queryer.irodsOptionsCall.offset, convey.ShouldEqual, 0)
		convey.So(queryer.irodsOptionsCall.opts, convey.ShouldResemble, IRODSPathOptions{
			FileType:         "cram",
			DeliverablesOnly: true,
			OrderBy:          irodsOrderByCreatedDesc,
			Since:            "2026-07-01T00:00:00Z",
			Until:            "2026-07-02T00:00:00Z",
		})
	})
}

func TestServerErrorEnvelopeB2(t *testing.T) {
	convey.Convey("B2.3: Given the queryer returns ErrNotFound, when GET /study/6568/samples is served, then status is 404 and code is not_found", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
				return nil, ErrNotFound
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusNotFound, "not_found")
	})

	convey.Convey("B2.4: Given the queryer returns ErrCacheNeverSynced, when GET /study/6568/samples is served, then status is 503 and code is cache_never_synced", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _ int, _ int) ([]Sample, error) {
				return nil, ErrCacheNeverSynced
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusServiceUnavailable, "cache_never_synced")
	})

	convey.Convey("B2.5: Given ResolveStudy returns ErrAmbiguous, when GET /resolve/study/x is served, then status is 409 and code is ambiguous", t, func() {
		queryer := &serverFakeQueryer{
			resolveStudyFunc: func(_ context.Context, _ string) (Match, error) {
				return Match{}, ErrAmbiguous
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/resolve/study/x")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusConflict, "ambiguous")
	})

	convey.Convey("B2.6: Given ClassifyIdentifier returns ErrUnsupportedIdentifier, when GET /classify/SQSCP is served, then status is 422 and code is unsupported_identifier", t, func() {
		queryer := &serverFakeQueryer{
			classifyIdentifierFunc: func(_ context.Context, _ string) (Match, error) {
				return Match{}, ErrUnsupportedIdentifier
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/classify/SQSCP")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusUnprocessableEntity, "unsupported_identifier")
	})
}

func assertMLWHErrorEnvelopeForTest(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	convey.So(response.Code, convey.ShouldEqual, status)

	var payload httpErrorEnvelope
	decodeMLWHJSONResponseForTest(t, response, &payload)
	convey.So(payload.Code, convey.ShouldEqual, code)
	convey.So(payload.Message, convey.ShouldNotBeBlank)
}

func TestServerEnrichB2(t *testing.T) {
	convey.Convey("B2.7: Given GET /enrich/6568 and an EnrichmentResult, then status is 200 and graph has no project or users keys", t, func() {
		queryer := &serverFakeQueryer{
			enrichFunc: func(_ context.Context, identifier string) (EnrichmentResult, error) {
				return EnrichmentResult{
					Identifier: identifier,
					Type:       KindStudyLimsID,
					Graph: EnrichmentGraph{
						Study: &Study{IDStudyLims: identifier},
					},
					Partial: false,
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/enrich/6568")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var payload map[string]any
		decodeMLWHJSONResponseForTest(t, response, &payload)
		graph, ok := payload["graph"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(payload["partial"], convey.ShouldEqual, false)
		convey.So(graph, convey.ShouldContainKey, "study")
		convey.So(graph, convey.ShouldNotContainKey, "project")
		convey.So(graph, convey.ShouldNotContainKey, "users")
	})
}

func TestServerClassifyJSONCasingE1(t *testing.T) {
	convey.Convey("E1.2: Given GET /classify/6568 returns a Match with a Study, then the top-level keys are snake_case and the nested study keeps its snake_case keys", t, func() {
		queryer := &serverFakeQueryer{
			classifyIdentifierFunc: func(_ context.Context, raw string) (Match, error) {
				return Match{
					Kind:      KindStudyLimsID,
					Canonical: raw,
					Study:     &Study{IDStudyLims: raw, Name: "Malaria genomics"},
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/classify/6568")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var payload map[string]any
		decodeMLWHJSONResponseForTest(t, response, &payload)

		convey.So(payload, convey.ShouldContainKey, "kind")
		convey.So(payload, convey.ShouldContainKey, "canonical")
		convey.So(payload, convey.ShouldContainKey, "study")
		convey.So(payload, convey.ShouldNotContainKey, "Kind")
		convey.So(payload, convey.ShouldNotContainKey, "Canonical")
		convey.So(payload, convey.ShouldNotContainKey, "Study")

		convey.So(payload["kind"], convey.ShouldEqual, string(KindStudyLimsID))
		convey.So(payload["canonical"], convey.ShouldEqual, "6568")

		study, ok := payload["study"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(study["id_study_lims"], convey.ShouldEqual, "6568")
		convey.So(study, convey.ShouldNotContainKey, "IDStudyLims")
	})
}

func TestServerSearchStudiesA4(t *testing.T) {
	convey.Convey("A4.1: Given a server over a fake Queryer whose SearchStudies returns two studies, when GET /search/study/malar is served with no auth, then status is 200 and the body is a 2-element Study array", t, func() {
		queryer := &serverFakeQueryer{
			searchStudiesFunc: func(_ context.Context, term string, _, _ int) ([]Study, error) {
				return []Study{
					{IDStudyLims: "1", Name: term + "-A"},
					{IDStudyLims: "2", Name: term + "-B"},
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/study/malar")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.searchCall.term, convey.ShouldEqual, "malar")
		convey.So(queryer.searchCall.limit, convey.ShouldEqual, mlwhSearchDefaultLimit)
		convey.So(queryer.searchCall.offset, convey.ShouldEqual, 0)

		var studies []Study
		decodeMLWHJSONResponseForTest(t, response, &studies)
		convey.So(studies, convey.ShouldResemble, []Study{
			{IDStudyLims: "1", Name: "malar-A"},
			{IDStudyLims: "2", Name: "malar-B"},
		})
	})

	convey.Convey("A4.2: Given GET /search/sample/acme?limit=2&offset=1, then the fake queryer receives term=acme, limit=2, offset=1", t, func() {
		queryer := &serverFakeQueryer{
			searchSamplesFunc: func(_ context.Context, _ string, _, _ int) ([]Sample, error) {
				return []Sample{}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/sample/acme?limit=2&offset=1")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.searchCall.term, convey.ShouldEqual, "acme")
		convey.So(queryer.searchCall.limit, convey.ShouldEqual, 2)
		convey.So(queryer.searchCall.offset, convey.ShouldEqual, 1)
	})
}

func TestServerSearchPaginationGuardA4(t *testing.T) {
	convey.Convey("A4.3: Given GET /search/study/malar?limit=1001, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			searchStudiesFunc: func(_ context.Context, _ string, _, _ int) ([]Study, error) {
				panic("queryer must not be called when limit exceeds the maximum")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/study/malar?limit=1001")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.searchCall.term, convey.ShouldBeEmpty)
	})

	convey.Convey("A4.4: Given GET /search/study/malar?limit=abc, then status is 400 with code bad_request", t, func() {
		queryer := &serverFakeQueryer{
			searchStudiesFunc: func(_ context.Context, _ string, _, _ int) ([]Study, error) {
				panic("queryer must not be called when limit is not an integer")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/study/malar?limit=abc")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
	})

	convey.Convey("Given GET /search/sample/acme?limit=1000 (the maximum), then status is 200 and the queryer receives limit=1000", t, func() {
		queryer := &serverFakeQueryer{
			searchSamplesFunc: func(_ context.Context, _ string, _, _ int) ([]Sample, error) {
				return []Sample{}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/sample/acme?limit=1000")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.searchCall.limit, convey.ShouldEqual, mlwhSearchMaxLimit)
	})

	convey.Convey("Given GET /search/study/malar?limit=-1, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			searchStudiesFunc: func(_ context.Context, _ string, _, _ int) ([]Study, error) {
				panic("queryer must not be called when limit is negative")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/study/malar?limit=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.searchCall.term, convey.ShouldBeEmpty)
	})

	convey.Convey("Given GET /search/sample/acme?limit=-1, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			searchSamplesFunc: func(_ context.Context, _ string, _, _ int) ([]Sample, error) {
				panic("queryer must not be called when limit is negative")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/sample/acme?limit=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.searchCall.term, convey.ShouldBeEmpty)
	})

	convey.Convey("Given GET /search/study/malar?offset=-1, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			searchStudiesFunc: func(_ context.Context, _ string, _, _ int) ([]Study, error) {
				panic("queryer must not be called when offset is negative")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/study/malar?offset=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.searchCall.term, convey.ShouldBeEmpty)
	})

	convey.Convey("Given GET /search/sample/acme?offset=-1, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			searchSamplesFunc: func(_ context.Context, _ string, _, _ int) ([]Sample, error) {
				panic("queryer must not be called when offset is negative")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/sample/acme?offset=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.searchCall.term, convey.ShouldBeEmpty)
	})
}

// TestServerIRODSFileTypeBadRequestGuardB2 is B2 acceptance test 4: a present
// but invalid file_type (empty, a '%', a '/', or a '_') on any iRODS list or
// count endpoint returns the 400 bad_request envelope BEFORE the queryer is
// reached. The serverFakeQueryer panics on every iRODS method, so a 400 with no
// panic proves the queryer was never called.
func TestServerIRODSFileTypeBadRequestGuardB2(t *testing.T) {
	invalidFileTypes := map[string]string{
		"empty":           "file_type=",
		"percent":         "file_type=%25",
		"path separator":  "file_type=a/b",
		"underscore":      "file_type=a_b",
		"whitespace only": "file_type=%20",
		"lone dot":        "file_type=.",
	}
	endpoints := []string{
		"/study/SZ/irods",
		"/study/SZ/irods/count",
		"/sample/SN/irods",
		"/sample/SN/irods/count",
		"/run/52553/irods",
		"/run/52553/irods/count",
	}

	for label, query := range invalidFileTypes {
		for _, endpoint := range endpoints {
			convey.Convey("B2.4: Given GET "+endpoint+"?"+query+" ("+label+"), then status is 400 bad_request and the queryer is not reached", t, func() {
				queryer := &serverFakeQueryer{}

				response := performMLWHRequestForTest(t, queryer, http.MethodGet, endpoint+"?"+query)

				assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
			})
		}
	}
}

func TestServerFetchAllPaginationGuard(t *testing.T) {
	convey.Convey("Given GET /study/SZ/detail?offset=-1, then status is 400 with code bad_request (not a 500/panic)", t, func() {
		client := newListSizingClientForTest(t, "SZ", 5)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/detail?offset=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
	})

	convey.Convey("Given GET /study/SZ/detail?limit=-1, then status is 400 with code bad_request", t, func() {
		client := newListSizingClientForTest(t, "SZ", 5)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/detail?limit=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
	})

	convey.Convey("Given GET /study/SZ/detail?offset=0, then status is 200 (the valid path still works)", t, func() {
		client := newListSizingClientForTest(t, "SZ", 5)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/detail?offset=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
	})

	convey.Convey("Given GET /study/SZ/samples?offset=-1, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _, _ int) ([]Sample, error) {
				panic("queryer must not be called when offset is negative")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/SZ/samples?offset=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.samplesForStudyCall.studyLimsID, convey.ShouldBeEmpty)
	})

	convey.Convey("Given GET /study/SZ/samples?limit=-1, then status is 400 with code bad_request and the queryer is not called", t, func() {
		queryer := &serverFakeQueryer{
			samplesForStudyFunc: func(_ context.Context, _ string, _, _ int) ([]Sample, error) {
				panic("queryer must not be called when limit is negative")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/SZ/samples?limit=-1")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		convey.So(queryer.samplesForStudyCall.studyLimsID, convey.ShouldBeEmpty)
	})
}

func TestServerCountEndpointsF3(t *testing.T) {
	convey.Convey("F3.1: Given a fake Queryer whose CountStudies returns Count{7}, when GET /studies/count is served, then status is 200 and body is {\"count\":7}", t, func() {
		queryer := &serverFakeQueryer{
			countStudiesFunc: func(_ context.Context) (Count, error) {
				return Count{Count: 7}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/studies/count")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var count Count
		decodeMLWHJSONResponseForTest(t, response, &count)
		convey.So(count, convey.ShouldResemble, Count{Count: 7})
		convey.So(strings.TrimSpace(response.Body.String()), convey.ShouldEqual, `{"count":7}`)
	})

	convey.Convey("F3.2: Given GET /study/6568/samples/count, then the queryer receives id=6568 and the body is {\"count\":N}", t, func() {
		queryer := &serverFakeQueryer{
			countSamplesForStudyFunc: func(_ context.Context, studyLimsID string) (Count, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")

				return Count{Count: 13}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples/count")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.countCall.studyLimsID, convey.ShouldEqual, "6568")

		var count Count
		decodeMLWHJSONResponseForTest(t, response, &count)
		convey.So(count, convey.ShouldResemble, Count{Count: 13})
	})

	convey.Convey("Given GET /study/6568/samples-with-data/count, then the queryer receives id=6568 and the body is {\"count\":N}", t, func() {
		queryer := &serverFakeQueryer{
			countSamplesWithDataFunc: func(_ context.Context, studyLimsID string) (Count, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")

				return Count{Count: 9}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples-with-data/count")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.countCall.studyLimsID, convey.ShouldEqual, "6568")

		var count Count
		decodeMLWHJSONResponseForTest(t, response, &count)
		convey.So(count, convey.ShouldResemble, Count{Count: 9})
	})

	convey.Convey("F3.3: Given GET /search/sample/acme/count, then the queryer receives term=acme", t, func() {
		queryer := &serverFakeQueryer{
			countSampleSearchFunc: func(_ context.Context, term string) (Count, error) {
				convey.So(term, convey.ShouldEqual, "acme")

				return Count{Count: 3}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/sample/acme/count")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.countCall.term, convey.ShouldEqual, "acme")

		var count Count
		decodeMLWHJSONResponseForTest(t, response, &count)
		convey.So(count, convey.ShouldResemble, Count{Count: 3})
	})

	convey.Convey("Given GET /search/study/malar/count, then the queryer receives term=malar and the body is {\"count\":N}", t, func() {
		queryer := &serverFakeQueryer{
			countStudySearchFunc: func(_ context.Context, term string) (Count, error) {
				convey.So(term, convey.ShouldEqual, "malar")

				return Count{Count: 2}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/search/study/malar/count")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(queryer.countCall.term, convey.ShouldEqual, "malar")

		var count Count
		decodeMLWHJSONResponseForTest(t, response, &count)
		convey.So(count, convey.ShouldResemble, Count{Count: 2})
	})
}

func TestServerStudyManifestPaginationHeadersUseQueryerCount(t *testing.T) {
	convey.Convey("Given a server over a fake Queryer whose manifest page has 2 rows and CountStudyManifest returns 3", t, func() {
		queryer := &serverFakeQueryer{
			studyManifestFunc: func(_ context.Context, studyLimsID, _ string, _ bool, limit, offset int) (StudyManifest, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "S1")
				convey.So(limit, convey.ShouldEqual, 2)
				convey.So(offset, convey.ShouldEqual, 0)

				return StudyManifest{
					IDStudyLims: studyLimsID,
					Rows: []ManifestRow{
						{Name: "sample-1", IDRun: 52553, Position: 1, TagIndex: 1},
						{Name: "sample-2", IDRun: 52553, Position: 1, TagIndex: 2},
					},
				}, nil
			},
			countStudyManifestFunc: func(_ context.Context, studyLimsID string) (Count, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "S1")

				return Count{Count: 3}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/manifest?limit=2&offset=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var manifest StudyManifest
		decodeMLWHJSONResponseForTest(t, response, &manifest)
		convey.So(manifest.Rows, convey.ShouldHaveLength, 2)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "3")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "2")
	})
}

func TestServerCountCacheNeverSyncedF3(t *testing.T) {
	convey.Convey("F3.5: Given GET /study/6568/samples/count where the queryer returns ErrCacheNeverSynced, then status is 503 with code cache_never_synced", t, func() {
		queryer := &serverFakeQueryer{
			countSamplesForStudyFunc: func(_ context.Context, _ string) (Count, error) {
				return Count{}, ErrCacheNeverSynced
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/6568/samples/count")

		assertMLWHErrorEnvelopeForTest(t, response, http.StatusServiceUnavailable, "cache_never_synced")
	})
}

func TestServerHealthD1(t *testing.T) {
	convey.Convey("D1.1: Given a server with any Queryer, when GET /health is served with no auth, then status is 200 and the body is {\"status\":\"ok\"}", t, func() {
		// The fake panics on every cache-backed method, so a 200 with the
		// expected body proves /health performs no cache read (D1.2).
		queryer := &serverFakeQueryer{}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/health")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(strings.TrimSpace(response.Body.String()), convey.ShouldEqual, `{"status":"ok"}`)
	})
}

func TestServerExpandJSONCasingE1(t *testing.T) {
	convey.Convey("E1.4: Given GET /expand/run_id/100 returns TaggedIDs, then each array element has snake_case keys kind and canonical", t, func() {
		queryer := &serverFakeQueryer{
			expandIdentifierFunc: func(_ context.Context, kind IdentifierKind, raw string) ([]TaggedID, error) {
				convey.So(kind, convey.ShouldEqual, KindRunID)
				convey.So(raw, convey.ShouldEqual, "100")

				return []TaggedID{
					{Kind: KindRunID, Canonical: raw},
					{Kind: KindSangerSampleName, Canonical: "DN1234"},
				}, nil
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/expand/run_id/100")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var payload []map[string]any
		decodeMLWHJSONResponseForTest(t, response, &payload)

		convey.So(payload, convey.ShouldHaveLength, 2)
		for _, element := range payload {
			convey.So(element, convey.ShouldContainKey, "kind")
			convey.So(element, convey.ShouldContainKey, "canonical")
			convey.So(element, convey.ShouldNotContainKey, "Kind")
			convey.So(element, convey.ShouldNotContainKey, "Canonical")
		}

		convey.So(payload[0]["kind"], convey.ShouldEqual, string(KindRunID))
		convey.So(payload[0]["canonical"], convey.ShouldEqual, "100")
	})
}

// irodsFileTypeFakeQueryer is a Queryer that also satisfies
// irodsPathsByFileTypeQueryer, recording which iRODS path each endpoint took and
// the file_type it received, so the dispatch test can assert the handler routes a
// present file_type to the filtered method and an absent one to the bare method.
type irodsFileTypeFakeQueryer struct {
	serverFakeQueryer
	bareStudyListCalled bool
	studyListFileType   string
	studyCountFileType  string
	sampleListFileType  string
	sampleCountFileType string
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForStudy(_ context.Context, _ string, _, _ int) ([]IRODSPath, error) {
	q.bareStudyListCalled = true

	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForStudy(_ context.Context, _ string) (Count, error) {
	return Count{}, nil
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForSample(_ context.Context, _ string, _, _ int) ([]IRODSPath, error) {
	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForSample(_ context.Context, _ string) (Count, error) {
	return Count{}, nil
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForStudyByFileType(_ context.Context, _, fileType string, _, _ int) ([]IRODSPath, error) {
	q.studyListFileType = fileType

	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForStudyByFileType(_ context.Context, _, fileType string) (Count, error) {
	q.studyCountFileType = fileType

	return Count{}, nil
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForSampleByFileType(_ context.Context, _, fileType string, _, _ int) ([]IRODSPath, error) {
	q.sampleListFileType = fileType

	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForSampleByFileType(_ context.Context, _, fileType string) (Count, error) {
	q.sampleCountFileType = fileType

	return Count{}, nil
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForStudyWithOptions(_ context.Context, _ string, opts IRODSPathOptions, _, _ int) ([]IRODSPath, error) {
	q.studyListFileType = opts.FileType

	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForSampleWithOptions(_ context.Context, _ string, opts IRODSPathOptions, _, _ int) ([]IRODSPath, error) {
	q.sampleListFileType = opts.FileType

	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) IRODSPathsForRunWithOptions(_ context.Context, _ string, _ IRODSPathOptions, _, _ int) ([]IRODSPath, error) {
	return []IRODSPath{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForStudyWithOptions(_ context.Context, _ string, opts IRODSPathOptions) (Count, error) {
	q.studyCountFileType = opts.FileType

	return Count{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForSampleWithOptions(_ context.Context, _ string, opts IRODSPathOptions) (Count, error) {
	q.sampleCountFileType = opts.FileType

	return Count{}, nil
}

func (q *irodsFileTypeFakeQueryer) CountIRODSPathsForRunWithOptions(_ context.Context, _ string, _ IRODSPathOptions) (Count, error) {
	return Count{}, nil
}

// TestServerIRODSFileTypeDispatchB2 proves the handler routes a present, valid
// file_type to the filtered ByFileType query path (and an absent file_type to
// the bare path), parameterising the existing endpoints rather than adding new
// ones. The fake records which path each endpoint took.
func TestServerIRODSFileTypeDispatchB2(t *testing.T) {
	convey.Convey("Given a server over a fake that records the file-type dispatch", t, func() {
		convey.Convey("when GET /study/SZ/irods?file_type=cram is served, then the filtered study path receives the normalised token", func() {
			queryer := &irodsFileTypeFakeQueryer{}

			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/SZ/irods?file_type=.CRAM")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(queryer.studyListFileType, convey.ShouldEqual, "cram")
			convey.So(queryer.studyCountFileType, convey.ShouldEqual, "cram")
		})

		convey.Convey("when GET /sample/SN/irods/count?file_type=bam is served, then the filtered sample count receives the normalised token", func() {
			queryer := &irodsFileTypeFakeQueryer{}

			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/sample/SN/irods/count?file_type=bam")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(queryer.sampleCountFileType, convey.ShouldEqual, "bam")
		})

		convey.Convey("when GET /study/SZ/irods is served with no file_type, then the bare path is taken", func() {
			queryer := &irodsFileTypeFakeQueryer{}

			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/SZ/irods")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(queryer.bareStudyListCalled, convey.ShouldBeTrue)
			convey.So(queryer.studyListFileType, convey.ShouldBeEmpty)
		})
	})
}

func TestServerUnauthenticatedReachabilityG3(t *testing.T) {
	convey.Convey("G3.1: Given a server over a synced cache (seeded with a malaria study) and no auth group", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "6568", "Malaria genomics", "Malaria study title", "Genomics", "Sponsor A")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when search, counts, freshness, health, and openapi are each requested with no auth, then all return 200", func() {
			for _, path := range []string{"/search/study/malar", "/studies/count", "/freshness", "/health", "/openapi.json"} {
				response := performMLWHRequestForTest(t, client, http.MethodGet, path)
				convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			}
		})

		convey.Convey("when GET /search/study/malar is served, then the seeded malaria study is returned (malar matches seeded studies)", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/search/study/malar")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var studies []Study
			decodeMLWHJSONResponseForTest(t, response, &studies)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].Name, convey.ShouldEqual, "Malaria genomics")
		})
	})
}

func TestServerFreshnessNeverSyncedReturns200G3(t *testing.T) {
	convey.Convey("G3.3: Given a server over a never-synced cache with no auth group, when GET /freshness is served, then status is 200 and not 503 (freshness degrades gracefully)", t, func() {
		client := newParityClient(t)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/freshness")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Code, convey.ShouldNotEqual, http.StatusServiceUnavailable)

		var freshness Freshness
		decodeMLWHJSONResponseForTest(t, response, &freshness)
		convey.So(freshness.Tables, convey.ShouldHaveLength, len(freshnessSyncTables))
		for _, table := range freshness.Tables {
			convey.So(table.EverSynced, convey.ShouldBeFalse)
		}
	})
}

func TestServerListSizingHeadersFirstPageE2(t *testing.T) {
	convey.Convey("E2.1: Given 25 matching rows and limit=10&offset=0, when GET /study/SZ/samples is served, then a 10-element array with X-Total-Count 25 and X-Next-Offset 10", t, func() {
		client := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/samples?limit=10&offset=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)
		convey.So(samples, convey.ShouldHaveLength, 10)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "25")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "10")
	})
}

func TestServerListSizingHeadersLastPageE2(t *testing.T) {
	convey.Convey("E2.2: Given 25 matching rows and limit=10&offset=20, when GET /study/SZ/samples is served, then a 5-element array with X-Total-Count 25 and X-Next-Offset -1", t, func() {
		client := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/samples?limit=10&offset=20")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)
		convey.So(samples, convey.ShouldHaveLength, 5)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "25")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "-1")
	})
}

func TestServerListSizingHeadersExactPageBoundaryE2(t *testing.T) {
	convey.Convey("E2 (boundary): Given 25 rows and limit=25&offset=0 (exactly one full page), when served, then X-Next-Offset is -1 because no rows remain", t, func() {
		client := newListSizingClientForTest(t, "SZ", 25)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/samples?limit=25&offset=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

		var samples []Sample
		decodeMLWHJSONResponseForTest(t, response, &samples)
		convey.So(samples, convey.ShouldHaveLength, 25)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "25")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "-1")
	})
}

func newListSizingClientForTest(t *testing.T, idStudyLims string, samples int) *Client {
	t.Helper()

	cache := openSQLiteSyncTestCache(t)
	seedListSizingStudy(t, cache.DB(), idStudyLims, 900, 900000, samples)

	return &Client{cache: cache, cacheReader: cacheReadDB(cache)}
}

// seedListSizingStudy seeds a study with the given number of distinct samples
// (each linked via library_samples) plus the sync state the availability reads
// need, so SamplesForStudy / IRODSPathsForStudy return a deterministic,
// pageable population for the list-sizing header tests (spec E2). It returns the
// LIMS study id. id_sample_tmp values start at base to keep fixtures disjoint.
func seedListSizingStudy(t *testing.T, db *sql.DB, idStudyLims string, idStudyTmp, base int64, samples int) {
	t.Helper()

	seedHierarchyStudy(t, db, idStudyTmp, idStudyLims)

	created := time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC)
	for i := range int64(samples) {
		idSampleTmp := base + i
		seedHierarchySample(t, db, idSampleTmp, idStudyLims, "sizing-"+formatInt(idSampleTmp))
		seedLibrarySample(t, db, "Standard", idSampleTmp, idStudyLims)
		seedIseqProductMetricsMirrorRow(t, db, 700000+idSampleTmp, idSampleTmp, 99000, 1, int(i), idStudyLims)
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, formatInt(700000+idSampleTmp), "/seq/99000", "99000_1#"+formatInt(idSampleTmp)+".cram", idSampleTmp, idStudyLims, created, "illumina")
		setIRODSLocationMirrorRunFields(t, db, 99000, 1, int(i), formatInt(700000+idSampleTmp))
	}

	seedB3AvailabilitySyncState(t, db)
}

func TestServerListSizingHeadersNotSetOnErrorE2(t *testing.T) {
	convey.Convey("E2 (error): Given a list error (never-synced cache), when GET /study/SZ/samples is served, then no sizing headers are written", t, func() {
		client := newParityClient(t)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/study/SZ/samples?limit=10&offset=0")

		convey.So(response.Code, convey.ShouldEqual, http.StatusServiceUnavailable)
		convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "")
		convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "")
	})
}

// B3 acceptance test 3: GET /run/:id/irods with limit=2&offset=0 over a run with
// six matching iRODS objects returns a 2-element JSON array with X-Total-Count: 6
// and X-Next-Offset: 2 (the sizing headers track the run-scope count, which is the
// same join with no LIMIT).
func TestServerIRODSPathsForRunPaginationHeadersB3(t *testing.T) {
	convey.Convey("B3.3: Given run 52553 with 6 matching iRODS objects served over HTTP", t, func() {
		cache := openSQLiteSyncTestCache(t)
		seedB3RunIRODSScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/run/52553/irods?limit=2&offset=0")

		convey.Convey("when GET /run/52553/irods?limit=2&offset=0 is served, then a 2-element array with X-Total-Count 6 and X-Next-Offset 2", func() {
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var paths []IRODSPath
			decodeMLWHJSONResponseForTest(t, response, &paths)
			convey.So(paths, convey.ShouldHaveLength, 2)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "6")
			convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "2")
		})
	})
}

func TestServerIRODSPathsForRunTotalCountIncludesMirrorOnlyRowsE1(t *testing.T) {
	convey.Convey("E1 regression: Given run 52553 with a mirror-only iRODS row", t, func() {
		cache := openSQLiteSyncTestCache(t)
		seedB3RunIRODSScenario(t, cache.DB())
		seedB3MirrorOnlyRunIRODSRow(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/run/52553/irods?limit=100&offset=0")

		convey.Convey("when GET /run/52553/irods is served, then X-Total-Count matches the mirror-scoped full list", func() {
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var paths []IRODSPath
			decodeMLWHJSONResponseForTest(t, response, &paths)
			convey.So(paths, convey.ShouldHaveLength, 7)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "7")
			convey.So(irodsProductIDs(paths), convey.ShouldContain, "mirror-only-52553")
		})
	})
}

func TestServerHealthDoesNotReadCacheD1(t *testing.T) {
	convey.Convey("D1.2: Given the server handler source, when audited, then /health performs no cache read (it does not call the Queryer)", t, func() {
		// Behavioural proof: a never-synced real cache would surface
		// cache_never_synced from any Queryer method; /health must still be a
		// cheap 200 because it never touches the Queryer.
		client := newParityClient(t)
		defer closeParityClientForTest(t, client)

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/health")

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(strings.TrimSpace(response.Body.String()), convey.ShouldEqual, `{"status":"ok"}`)
	})
}
