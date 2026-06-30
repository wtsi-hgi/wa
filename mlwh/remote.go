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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const remoteClientDefaultTimeout = 30 * time.Second

var _ Queryer = (*RemoteClient)(nil)

// RemoteClient queries an mlwh REST server through the Queryer interface.
type RemoteClient struct {
	baseURL    string
	httpClient *http.Client
	token      string
	endpoints  map[string]Endpoint
}

// NewRemoteClient builds a RemoteClient for an mlwh REST server.
func NewRemoteClient(cfg RemoteConfig) (*RemoteClient, error) {
	baseURL, err := normalizeRemoteBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = remoteClientDefaultTimeout
	}

	httpClient, err := newRemoteHTTPClient(timeout, cfg.CACert)
	if err != nil {
		return nil, err
	}

	return &RemoteClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		token:      cfg.Token,
		endpoints:  remoteEndpointMap(),
	}, nil
}

// Close releases idle transport resources held by the RemoteClient.
func (rc *RemoteClient) Close() error {
	if rc == nil || rc.httpClient == nil {
		return nil
	}

	rc.httpClient.CloseIdleConnections()

	return nil
}

// ClassifyIdentifier classifies a raw identifier through the remote server.
func (rc *RemoteClient) ClassifyIdentifier(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ClassifyIdentifier", []string{raw}, nil)
}

// ResolveSample resolves a sample identifier through the remote server.
func (rc *RemoteClient) ResolveSample(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ResolveSample", []string{raw}, nil)
}

// ResolveSampleName resolves a Sanger sample name through the remote server.
func (rc *RemoteClient) ResolveSampleName(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ResolveSampleName", []string{raw}, nil)
}

// ResolveStudy resolves a study identifier through the remote server.
func (rc *RemoteClient) ResolveStudy(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ResolveStudy", []string{raw}, nil)
}

// ResolveRun resolves a run identifier through the remote server.
func (rc *RemoteClient) ResolveRun(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ResolveRun", []string{raw}, nil)
}

// ResolveLibrary resolves a library identifier through the remote server.
func (rc *RemoteClient) ResolveLibrary(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ResolveLibrary", []string{raw}, nil)
}

// ResolveLibraryIdentifier resolves a library ID or LIMS ID through the remote server.
func (rc *RemoteClient) ResolveLibraryIdentifier(ctx context.Context, raw string) (Match, error) {
	return remoteCall[Match](rc, ctx, "ResolveLibraryIdentifier", []string{raw}, nil)
}

// AllStudies lists studies through the remote server.
func (rc *RemoteClient) AllStudies(ctx context.Context, limit, offset int) ([]Study, error) {
	return remoteCall[[]Study](rc, ctx, "AllStudies", nil, remotePagination(limit, offset))
}

func remotePagination(limit, offset int) url.Values {
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("offset", strconv.Itoa(offset))

	return values
}

// AllStudiesPage is the Page[Study] variant of AllStudies: it returns the same
// page of rows (Page.Items) plus the list-sizing metadata from the X-Total-Count
// / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) AllStudiesPage(ctx context.Context, limit, offset int) (Page[Study], error) {
	return remoteCallPage[Study](rc, ctx, "AllStudies", nil, remotePagination(limit, offset))
}

// SamplesForStudy lists samples for a study through the remote server.
func (rc *RemoteClient) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SamplesForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// SamplesForStudyPage is the Page[Sample] variant of SamplesForStudy: it returns
// the same page of rows (Page.Items) plus the list-sizing metadata from the
// X-Total-Count / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SamplesForStudyPage(ctx context.Context, studyLimsID string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SamplesForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// SamplesForRun lists samples for a run through the remote server.
func (rc *RemoteClient) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SamplesForRun", []string{idRun}, remotePagination(limit, offset))
}

// SamplesForRunPage is the Page[Sample] variant of SamplesForRun: it returns the
// same page of rows (Page.Items) plus the list-sizing metadata from the
// X-Total-Count / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SamplesForRunPage(ctx context.Context, idRun string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SamplesForRun", []string{idRun}, remotePagination(limit, offset))
}

// SamplesForLibrary lists samples for a library type and study through the remote server.
func (rc *RemoteClient) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SamplesForLibrary", []string{pipelineIDLims, studyLimsID}, remotePagination(limit, offset))
}

// SamplesForLibraryPage is the Page[Sample] variant of SamplesForLibrary: it
// returns the same page of rows (Page.Items) plus the list-sizing metadata from
// the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) SamplesForLibraryPage(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SamplesForLibrary", []string{pipelineIDLims, studyLimsID}, remotePagination(limit, offset))
}

// SamplesForLibraryID lists samples for a library ID through the remote server.
func (rc *RemoteClient) SamplesForLibraryID(ctx context.Context, libraryID string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SamplesForLibraryID", []string{libraryID}, remotePagination(limit, offset))
}

// SamplesForLibraryIDPage is the Page[Sample] variant of SamplesForLibraryID: it
// returns the same page of rows (Page.Items) plus the list-sizing metadata from
// the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) SamplesForLibraryIDPage(ctx context.Context, libraryID string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SamplesForLibraryID", []string{libraryID}, remotePagination(limit, offset))
}

// SamplesForLibraryLimsID lists samples for a library LIMS ID through the remote server.
func (rc *RemoteClient) SamplesForLibraryLimsID(ctx context.Context, idLibraryLims string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SamplesForLibraryLimsID", []string{idLibraryLims}, remotePagination(limit, offset))
}

// SamplesForLibraryLimsIDPage is the Page[Sample] variant of
// SamplesForLibraryLimsID: it returns the same page of rows (Page.Items) plus the
// list-sizing metadata from the X-Total-Count / X-Next-Offset response headers
// (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SamplesForLibraryLimsIDPage(ctx context.Context, idLibraryLims string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SamplesForLibraryLimsID", []string{idLibraryLims}, remotePagination(limit, offset))
}

// SamplesForLibraryType lists samples for a library type through the remote server.
func (rc *RemoteClient) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SamplesForLibraryType", []string{pipelineIDLims}, remotePagination(limit, offset))
}

// SamplesForLibraryTypePage is the Page[Sample] variant of
// SamplesForLibraryType: it returns the same page of rows (Page.Items) plus the
// list-sizing metadata from the X-Total-Count / X-Next-Offset response headers
// (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SamplesForLibraryTypePage(ctx context.Context, pipelineIDLims string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SamplesForLibraryType", []string{pipelineIDLims}, remotePagination(limit, offset))
}

// LibrariesForStudy lists libraries for a study through the remote server.
func (rc *RemoteClient) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Library, error) {
	return remoteCall[[]Library](rc, ctx, "LibrariesForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// LibrariesForStudyPage is the Page[Library] variant of LibrariesForStudy: it
// returns the same page of rows (Page.Items) plus the list-sizing metadata from
// the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) LibrariesForStudyPage(ctx context.Context, studyLimsID string, limit, offset int) (Page[Library], error) {
	return remoteCallPage[Library](rc, ctx, "LibrariesForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// RunsForStudy lists runs for a study through the remote server.
func (rc *RemoteClient) RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Run, error) {
	return remoteCall[[]Run](rc, ctx, "RunsForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// RunsForStudyPage is the Page[Run] variant of RunsForStudy: it returns the same
// page of rows (Page.Items) plus the list-sizing metadata from the X-Total-Count
// / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) RunsForStudyPage(ctx context.Context, studyLimsID string, limit, offset int) (Page[Run], error) {
	return remoteCallPage[Run](rc, ctx, "RunsForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// StudyOverview returns a study's overview aggregate through the remote server.
func (rc *RemoteClient) StudyOverview(ctx context.Context, studyLimsID string) (StudyOverview, error) {
	return remoteCall[StudyOverview](rc, ctx, "StudyOverview", []string{studyLimsID}, nil)
}

// RunOverview returns a run's overview aggregate through the remote server.
func (rc *RemoteClient) RunOverview(ctx context.Context, idRun string) (RunOverview, error) {
	return remoteCall[RunOverview](rc, ctx, "RunOverview", []string{idRun}, nil)
}

// RunStatus returns a run's within-sequencing status timeline through the remote
// server.
func (rc *RemoteClient) RunStatus(ctx context.Context, idRun string) (RunStatusTimeline, error) {
	return remoteCall[RunStatusTimeline](rc, ctx, "RunStatus", []string{idRun}, nil)
}

// SampleProgress returns a sample's unified progress (baseline, milestone
// timeline and per-run status) through the remote server. id is the Sanger
// sample name.
func (rc *RemoteClient) SampleProgress(ctx context.Context, sangerName string) (SampleProgress, error) {
	return remoteCall[SampleProgress](rc, ctx, "SampleProgress", []string{sangerName}, nil)
}

// StatusBreakdown returns a study's per-baseline-phase status breakdown (the
// distinct and per-platform partitions, the detailed-timeline count and the
// freshness) through the remote server. id is the LIMS study id.
func (rc *RemoteClient) StatusBreakdown(ctx context.Context, studyLimsID string) (StatusBreakdown, error) {
	return remoteCall[StatusBreakdown](rc, ctx, "StatusBreakdown", []string{studyLimsID}, nil)
}

// SamplesWithData lists a study's samples-with-data through the remote server.
func (rc *RemoteClient) SamplesWithData(ctx context.Context, studyLimsID string, limit, offset int) ([]SampleWithData, error) {
	return remoteCall[[]SampleWithData](rc, ctx, "SamplesWithData", []string{studyLimsID}, remotePagination(limit, offset))
}

// SamplesWithDataPage is the Page[SampleWithData] variant of SamplesWithData: it
// returns the same page of rows (Page.Items) plus the list-sizing metadata from
// the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) SamplesWithDataPage(ctx context.Context, studyLimsID string, limit, offset int) (Page[SampleWithData], error) {
	return remoteCallPage[SampleWithData](rc, ctx, "SamplesWithData", []string{studyLimsID}, remotePagination(limit, offset))
}

// SamplesWithoutData lists a study's samples-without-data through the remote server.
func (rc *RemoteClient) SamplesWithoutData(ctx context.Context, studyLimsID string, limit, offset int) ([]SampleWithData, error) {
	return remoteCall[[]SampleWithData](rc, ctx, "SamplesWithoutData", []string{studyLimsID}, remotePagination(limit, offset))
}

// SamplesWithoutDataPage is the Page[SampleWithData] variant of
// SamplesWithoutData: it returns the same page of rows (Page.Items) plus the
// list-sizing metadata from the X-Total-Count / X-Next-Offset response headers
// (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SamplesWithoutDataPage(ctx context.Context, studyLimsID string, limit, offset int) (Page[SampleWithData], error) {
	return remoteCallPage[SampleWithData](rc, ctx, "SamplesWithoutData", []string{studyLimsID}, remotePagination(limit, offset))
}

// SamplesWithDataSince lists the distinct samples whose study-scoped iRODS data
// was added in the half-open window [since, until) through the remote server. It
// is the windowed variant of SamplesWithData and issues the same
// /study/:id/samples-with-data endpoint with the since/until RFC3339 query string
// alongside limit/offset (so there is one endpoint, parameterised by the window);
// an empty since requests the all-time list. The server validates the bounds and
// returns 400 for a malformed value.
func (rc *RemoteClient) SamplesWithDataSince(ctx context.Context, studyLimsID, since, until string, limit, offset int) ([]SampleWithData, error) {
	return remoteCall[[]SampleWithData](rc, ctx, "SamplesWithData", []string{studyLimsID}, remotePaginationWithAddedWindow(limit, offset, since, until))
}

// remotePaginationWithAddedWindow builds the query values for the windowed
// samples-with-data list: the limit/offset pagination controls plus the optional
// since/until [since, until) bounds, an empty bound omitted (matching the
// all-time SamplesWithData call).
func remotePaginationWithAddedWindow(limit, offset int, since, until string) url.Values {
	values := remotePagination(limit, offset)
	if since != "" {
		values.Set("since", since)
	}
	if until != "" {
		values.Set("until", until)
	}

	return values
}

// LanesForSample lists lanes for a sample through the remote server.
func (rc *RemoteClient) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]Lane, error) {
	return remoteCall[[]Lane](rc, ctx, "LanesForSample", []string{sangerName}, remotePagination(limit, offset))
}

// LanesForSamplePage is the Page[Lane] variant of LanesForSample: it returns the
// same page of rows (Page.Items) plus the list-sizing metadata from the
// X-Total-Count / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) LanesForSamplePage(ctx context.Context, sangerName string, limit, offset int) (Page[Lane], error) {
	return remoteCallPage[Lane](rc, ctx, "LanesForSample", []string{sangerName}, remotePagination(limit, offset))
}

// IRODSPathsForSample lists iRODS paths for a sample through the remote server.
func (rc *RemoteClient) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]IRODSPath, error) {
	return rc.IRODSPathsForSampleByFileType(ctx, sangerName, "", limit, offset)
}

// IRODSPathsForSampleByFileType lists iRODS paths for a sample through the remote
// server, optionally filtered to a file-type suffix. It is the filtered variant
// of IRODSPathsForSample and issues the same /sample/:id/irods endpoint with the
// file_type query param alongside limit/offset (so there is one endpoint,
// parameterised by the filter); an empty fileType requests all file types. The
// server validates the file_type and returns 400 for an invalid value.
func (rc *RemoteClient) IRODSPathsForSampleByFileType(ctx context.Context, sangerName, fileType string, limit, offset int) ([]IRODSPath, error) {
	return remoteCall[[]IRODSPath](rc, ctx, "IRODSPathsForSample", []string{sangerName}, remotePaginationWithFileType(limit, offset, fileType))
}

// remotePaginationWithFileType builds the query values for an iRODS list: the
// limit/offset pagination controls plus the optional file_type filter, an empty
// fileType omitted (matching the all-file-types call).
func remotePaginationWithFileType(limit, offset int, fileType string) url.Values {
	values := remotePagination(limit, offset)
	if fileType != "" {
		values.Set("file_type", fileType)
	}

	return values
}

// IRODSPathsForStudy lists iRODS paths for a study through the remote server.
func (rc *RemoteClient) IRODSPathsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]IRODSPath, error) {
	return rc.IRODSPathsForStudyByFileType(ctx, studyLimsID, "", limit, offset)
}

// IRODSPathsForStudyByFileType lists iRODS paths for a study through the remote
// server, optionally filtered to a file-type suffix, the same way as
// IRODSPathsForSampleByFileType.
func (rc *RemoteClient) IRODSPathsForStudyByFileType(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]IRODSPath, error) {
	return remoteCall[[]IRODSPath](rc, ctx, "IRODSPathsForStudy", []string{studyLimsID}, remotePaginationWithFileType(limit, offset, fileType))
}

// IRODSPathsForStudyPage is the Page[IRODSPath] variant of IRODSPathsForStudy: it
// returns the same page of rows (Page.Items) plus the list-sizing metadata from
// the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) IRODSPathsForStudyPage(ctx context.Context, studyLimsID string, limit, offset int) (Page[IRODSPath], error) {
	return remoteCallPage[IRODSPath](rc, ctx, "IRODSPathsForStudy", []string{studyLimsID}, remotePagination(limit, offset))
}

// IRODSPathsForRun lists the iRODS data objects on a run through the remote server
// (spec B3), optionally filtered to a file-type suffix. It issues the
// /run/:id/irods endpoint with the file_type query param alongside limit/offset (so
// there is one endpoint, parameterised by the filter); an empty fileType requests
// all file types. The server validates the file_type and returns 400 for an invalid
// value, and resolves :id through the run space (ResolveRun).
func (rc *RemoteClient) IRODSPathsForRun(ctx context.Context, idRun, fileType string, limit, offset int) ([]IRODSPath, error) {
	return remoteCall[[]IRODSPath](rc, ctx, "IRODSPathsForRun", []string{idRun}, remotePaginationWithFileType(limit, offset, fileType))
}

// IRODSPathsForRunPage returns an unfiltered all-file-types page of iRODS data
// objects for a run, plus the list-sizing metadata from the X-Total-Count /
// X-Next-Offset response headers (Page.Total / Page.NextOffset). It does not
// accept a file-type filter.
func (rc *RemoteClient) IRODSPathsForRunPage(ctx context.Context, idRun string, limit, offset int) (Page[IRODSPath], error) {
	return remoteCallPage[IRODSPath](rc, ctx, "IRODSPathsForRun", []string{idRun}, remotePagination(limit, offset))
}

// StudyManifest returns a study's product manifest envelope through the remote
// server. It is a PLAIN remoteCall returning the StudyManifest envelope (not a
// bare-slice Page[T]), since the manifest is an envelope (study metadata once plus
// the page of product rows). The query forwards the limit/offset pagination plus
// the optional with_irods flag (omitted when false) and file_type filter (omitted
// when empty), so the server applies the same product-grain list and the same
// sizing headers; the server validates the file_type and returns 400 for an
// invalid value.
func (rc *RemoteClient) StudyManifest(ctx context.Context, studyLimsID, fileType string, withIRODS bool, limit, offset int) (StudyManifest, error) {
	return remoteCall[StudyManifest](rc, ctx, "StudyManifest", []string{studyLimsID}, remoteManifestQuery(limit, offset, withIRODS, fileType))
}

// remoteManifestQuery builds the query values for the study manifest list: the
// limit/offset pagination controls plus the optional with_irods flag (set only
// when true) and file_type filter (set only when non-empty), matching the
// bare/with-irods/filtered call forms.
func remoteManifestQuery(limit, offset int, withIRODS bool, fileType string) url.Values {
	values := remotePaginationWithFileType(limit, offset, fileType)
	if withIRODS {
		values.Set("with_irods", "true")
	}

	return values
}

// StudiesForSample lists studies for a sample through the remote server.
func (rc *RemoteClient) StudiesForSample(ctx context.Context, sangerName string) ([]Study, error) {
	return remoteCall[[]Study](rc, ctx, "StudiesForSample", []string{sangerName}, nil)
}

// StudiesForFacultySponsor lists the studies of a named PI/sponsor through the
// remote server (the named study.faculty_sponsor, case-insensitive substring),
// each as a PersonStudy with an empty Role.
func (rc *RemoteClient) StudiesForFacultySponsor(ctx context.Context, name string, limit, offset int) ([]PersonStudy, error) {
	return remoteCall[[]PersonStudy](rc, ctx, "StudiesForFacultySponsor", []string{name}, remotePagination(limit, offset))
}

// StudiesForFacultySponsorPage is the Page[PersonStudy] variant of
// StudiesForFacultySponsor: it returns the same page of rows (Page.Items) plus
// the list-sizing metadata from the X-Total-Count / X-Next-Offset response
// headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) StudiesForFacultySponsorPage(ctx context.Context, name string, limit, offset int) (Page[PersonStudy], error) {
	return remoteCallPage[PersonStudy](rc, ctx, "StudiesForFacultySponsor", []string{name}, remotePagination(limit, offset))
}

// CountStudiesForFacultySponsor counts the studies of a named PI/sponsor through
// the remote server, the count counterpart of StudiesForFacultySponsor (the same
// faculty_sponsor case-insensitive substring match with no LIMIT), so it equals
// that list's length when all rows are fetched.
func (rc *RemoteClient) CountStudiesForFacultySponsor(ctx context.Context, name string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountStudiesForFacultySponsor", []string{name}, nil)
}

// StudiesForUser lists the studies a person is a study_users role member of
// through the remote server (matched case-insensitively as a substring of
// name/login/email), each as a PersonStudy carrying the matched role. role is the
// raw, comma-separated override of the default role set, forwarded as the role
// query param (omitted when empty so the server applies the default set); the same
// study may appear under multiple roles, de-duplicated to one row per
// (id_study_lims, role).
func (rc *RemoteClient) StudiesForUser(ctx context.Context, person, role string, limit, offset int) ([]PersonStudy, error) {
	return remoteCall[[]PersonStudy](rc, ctx, "StudiesForUser", []string{person}, remotePaginationWithRole(limit, offset, role))
}

// remotePaginationWithRole builds the query values for the studies-by-user list:
// the limit/offset pagination controls plus the optional role override, an empty
// role omitted (matching the default-role-set call).
func remotePaginationWithRole(limit, offset int, role string) url.Values {
	values := remotePagination(limit, offset)
	if role != "" {
		values.Set("role", role)
	}

	return values
}

// StudiesForUserPage is the Page[PersonStudy] variant of StudiesForUser: it
// returns the same page of rows (Page.Items) plus the list-sizing metadata from
// the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) StudiesForUserPage(ctx context.Context, person, role string, limit, offset int) (Page[PersonStudy], error) {
	return remoteCallPage[PersonStudy](rc, ctx, "StudiesForUser", []string{person}, remotePaginationWithRole(limit, offset, role))
}

// CountStudiesForUser counts the studies a person is a study_users role member of
// through the remote server, the count counterpart of StudiesForUser (the same
// name/login/email substring match and role filter, counting the distinct
// (id_study_lims, role) matches with no LIMIT), so it equals that list's length
// when all rows are fetched. role is forwarded as the role query param (omitted
// when empty so the server applies the default set).
func (rc *RemoteClient) CountStudiesForUser(ctx context.Context, person, role string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountStudiesForUser", []string{person}, remoteRole(role))
}

// remoteRole builds the role query values for a studies-by-user count, omitting an
// empty role so a default-role-set request sends no query string (matching the
// bare default call).
func remoteRole(role string) url.Values {
	values := url.Values{}
	if role != "" {
		values.Set("role", role)
	}

	return values
}

// ResolvePerson lists the distinct candidate people (faculty_sponsor and
// study_users) matching the term as a case-insensitive substring through the remote
// server, each as a PersonCandidate carrying its source, stored form and distinct
// study count.
func (rc *RemoteClient) ResolvePerson(ctx context.Context, term string, limit, offset int) ([]PersonCandidate, error) {
	return remoteCall[[]PersonCandidate](rc, ctx, "ResolvePerson", []string{term}, remotePagination(limit, offset))
}

// ResolvePersonPage is the Page[PersonCandidate] variant of ResolvePerson: it
// returns the same page of candidates (Page.Items) plus the list-sizing metadata
// from the X-Total-Count / X-Next-Offset response headers (Page.Total /
// Page.NextOffset).
func (rc *RemoteClient) ResolvePersonPage(ctx context.Context, term string, limit, offset int) (Page[PersonCandidate], error) {
	return remoteCallPage[PersonCandidate](rc, ctx, "ResolvePerson", []string{term}, remotePagination(limit, offset))
}

// CountResolvePerson counts the distinct candidate people matching the term across
// both sources through the remote server, the count counterpart of ResolvePerson
// (the same case-insensitive substring match with no LIMIT), so it equals that
// list's length when all rows are fetched.
func (rc *RemoteClient) CountResolvePerson(ctx context.Context, term string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountResolvePerson", []string{term}, nil)
}

// FindSamplesBySangerID finds samples by Sanger sample ID through the remote server.
func (rc *RemoteClient) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "FindSamplesBySangerID", []string{sangerID}, nil)
}

// FindSamplesByIDSampleLims finds samples by LIMS sample ID through the remote server.
func (rc *RemoteClient) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "FindSamplesByIDSampleLims", []string{idSampleLims}, nil)
}

// FindSamplesByAccessionNumber finds samples by accession number through the remote server.
func (rc *RemoteClient) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "FindSamplesByAccessionNumber", []string{accessionNumber}, nil)
}

// FindSamplesBySupplierName finds samples by supplier name through the remote server.
func (rc *RemoteClient) FindSamplesBySupplierName(ctx context.Context, supplierName string) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "FindSamplesBySupplierName", []string{supplierName}, nil)
}

// FindSamplesByLibraryType finds samples by library type through the remote server.
func (rc *RemoteClient) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "FindSamplesByLibraryType", []string{libraryType}, nil)
}

// ExpandIdentifier expands an identifier through the remote server.
func (rc *RemoteClient) ExpandIdentifier(ctx context.Context, kind IdentifierKind, canonical string) ([]TaggedID, error) {
	return remoteCall[[]TaggedID](rc, ctx, "ExpandIdentifier", []string{string(kind), canonical}, nil)
}

// ExpandSearchValues expands search values through the remote server.
func (rc *RemoteClient) ExpandSearchValues(ctx context.Context, kind IdentifierKind, canonical string) (SearchValues, error) {
	return remoteCall[SearchValues](rc, ctx, "ExpandSearchValues", []string{string(kind), canonical}, nil)
}

// ExpandSampleSearchValues expands sample search values through the remote server.
func (rc *RemoteClient) ExpandSampleSearchValues(ctx context.Context, kind IdentifierKind, canonical string) ([]string, error) {
	return remoteCall[[]string](rc, ctx, "ExpandSampleSearchValues", []string{string(kind), canonical}, nil)
}

// SearchStudies runs a study substring search through the remote server.
func (rc *RemoteClient) SearchStudies(ctx context.Context, term string, limit, offset int) ([]Study, error) {
	return remoteCall[[]Study](rc, ctx, "SearchStudies", []string{term}, remotePagination(limit, offset))
}

// SearchStudiesPage is the Page[Study] variant of SearchStudies: it returns the
// same page of rows (Page.Items) plus the list-sizing metadata from the
// X-Total-Count / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SearchStudiesPage(ctx context.Context, term string, limit, offset int) (Page[Study], error) {
	return remoteCallPage[Study](rc, ctx, "SearchStudies", []string{term}, remotePagination(limit, offset))
}

// SearchSamples runs a sample substring search through the remote server.
func (rc *RemoteClient) SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error) {
	return remoteCall[[]Sample](rc, ctx, "SearchSamples", []string{term}, remotePagination(limit, offset))
}

// SearchSamplesPage is the Page[Sample] variant of SearchSamples: it returns the
// same page of rows (Page.Items) plus the list-sizing metadata from the
// X-Total-Count / X-Next-Offset response headers (Page.Total / Page.NextOffset).
func (rc *RemoteClient) SearchSamplesPage(ctx context.Context, term string, limit, offset int) (Page[Sample], error) {
	return remoteCallPage[Sample](rc, ctx, "SearchSamples", []string{term}, remotePagination(limit, offset))
}

// CountStudySearch counts the studies matching term through the remote server.
func (rc *RemoteClient) CountStudySearch(ctx context.Context, term string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountStudySearch", []string{term}, nil)
}

// CountSampleSearch counts the samples matching term through the remote server.
func (rc *RemoteClient) CountSampleSearch(ctx context.Context, term string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSampleSearch", []string{term}, nil)
}

// CountStudies counts the mirrored studies through the remote server.
func (rc *RemoteClient) CountStudies(ctx context.Context) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountStudies", nil, nil)
}

// CountSamplesForStudy counts the distinct samples for a study through the remote server.
func (rc *RemoteClient) CountSamplesForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesForStudy", []string{studyLimsID}, nil)
}

// CountSamplesWithData counts the distinct samples-with-data for a study through the remote server.
func (rc *RemoteClient) CountSamplesWithData(ctx context.Context, studyLimsID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesWithData", []string{studyLimsID}, nil)
}

// CountSamplesWithDataSince counts the distinct samples whose study-scoped iRODS
// data was added in the half-open window [since, until) through the remote
// server. It is the windowed variant of CountSamplesWithData and issues the same
// /study/:id/samples-with-data/count endpoint with the since/until RFC3339 query
// string (so there is one endpoint, parameterised by the window); an empty since
// requests the all-time count. The server validates the bounds and returns 400
// for a malformed value.
func (rc *RemoteClient) CountSamplesWithDataSince(ctx context.Context, studyLimsID, since, until string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesWithData", []string{studyLimsID}, remoteAddedWindow(since, until))
}

// remoteAddedWindow builds the since/until query values for the windowed
// samples-with-data count, omitting an empty bound so an all-time request sends
// no query string (matching the bare CountSamplesWithData call).
func remoteAddedWindow(since, until string) url.Values {
	values := url.Values{}
	if since != "" {
		values.Set("since", since)
	}
	if until != "" {
		values.Set("until", until)
	}

	return values
}

// CountSamplesForRun counts the distinct samples on a run through the remote server.
func (rc *RemoteClient) CountSamplesForRun(ctx context.Context, idRun string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesForRun", []string{idRun}, nil)
}

// CountSamplesForLibrary counts the distinct samples in a library type and study through the remote server.
func (rc *RemoteClient) CountSamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesForLibrary", []string{pipelineIDLims, studyLimsID}, nil)
}

// CountSamplesForLibraryID counts the distinct samples for a library id through the remote server.
func (rc *RemoteClient) CountSamplesForLibraryID(ctx context.Context, libraryID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesForLibraryID", []string{libraryID}, nil)
}

// CountSamplesForLibraryLimsID counts the distinct samples for a LIMS library id through the remote server.
func (rc *RemoteClient) CountSamplesForLibraryLimsID(ctx context.Context, idLibraryLims string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesForLibraryLimsID", []string{idLibraryLims}, nil)
}

// CountSamplesForLibraryType counts the distinct samples for a library type through the remote server.
func (rc *RemoteClient) CountSamplesForLibraryType(ctx context.Context, pipelineIDLims string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountSamplesForLibraryType", []string{pipelineIDLims}, nil)
}

// CountRunsForStudy counts the distinct runs for a study through the remote server.
func (rc *RemoteClient) CountRunsForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountRunsForStudy", []string{studyLimsID}, nil)
}

// CountStudyManifest counts the distinct products in a study's manifest through
// the remote server (spec C2), the count counterpart of StudyManifest. It is a
// plain remoteCall returning the Count envelope; the figure is product-grained
// (unaffected by the manifest's with_irods / file_type options, which this
// endpoint does not take), so it equals the manifest list's row count.
func (rc *RemoteClient) CountStudyManifest(ctx context.Context, studyLimsID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountStudyManifest", []string{studyLimsID}, nil)
}

// CountLibrariesForStudy counts the distinct libraries for a study through the remote server.
func (rc *RemoteClient) CountLibrariesForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountLibrariesForStudy", []string{studyLimsID}, nil)
}

// CountLanesForSample counts the distinct lanes for a sample through the remote server.
func (rc *RemoteClient) CountLanesForSample(ctx context.Context, sangerName string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountLanesForSample", []string{sangerName}, nil)
}

// CountIRODSPathsForSample counts the distinct iRODS data objects for a sample through the remote server.
func (rc *RemoteClient) CountIRODSPathsForSample(ctx context.Context, sangerName string) (Count, error) {
	return rc.CountIRODSPathsForSampleByFileType(ctx, sangerName, "")
}

// CountIRODSPathsForSampleByFileType counts the distinct iRODS data objects for a
// sample through the remote server, optionally filtered to a file-type suffix. It
// is the filtered variant of CountIRODSPathsForSample and issues the same
// /sample/:id/irods/count endpoint with the file_type query param (so there is
// one endpoint, parameterised by the filter); an empty fileType requests an
// all-file-types count. The server validates the file_type and returns 400 for an
// invalid value.
func (rc *RemoteClient) CountIRODSPathsForSampleByFileType(ctx context.Context, sangerName, fileType string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountIRODSPathsForSample", []string{sangerName}, remoteFileType(fileType))
}

// remoteFileType builds the file_type query values for a filtered iRODS count,
// omitting an empty fileType so an all-file-types request sends no query string
// (matching the bare count call).
func remoteFileType(fileType string) url.Values {
	values := url.Values{}
	if fileType != "" {
		values.Set("file_type", fileType)
	}

	return values
}

// CountIRODSPathsForStudy counts the distinct iRODS data objects for a study through the remote server.
func (rc *RemoteClient) CountIRODSPathsForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	return rc.CountIRODSPathsForStudyByFileType(ctx, studyLimsID, "")
}

// CountIRODSPathsForStudyByFileType counts the distinct iRODS data objects for a
// study through the remote server, optionally filtered to a file-type suffix, the
// same way as CountIRODSPathsForSampleByFileType.
func (rc *RemoteClient) CountIRODSPathsForStudyByFileType(ctx context.Context, studyLimsID, fileType string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountIRODSPathsForStudy", []string{studyLimsID}, remoteFileType(fileType))
}

// CountIRODSPathsForRun counts the iRODS data objects on a run through the remote
// server (spec B3), optionally filtered to a file-type suffix. It issues the
// /run/:id/irods/count endpoint with the file_type query param; an empty fileType
// counts all file types. The server validates the file_type and resolves :id
// through the run space (ResolveRun).
func (rc *RemoteClient) CountIRODSPathsForRun(ctx context.Context, idRun, fileType string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountIRODSPathsForRun", []string{idRun}, remoteFileType(fileType))
}

// CountFindSamplesBySangerID counts the samples matching a Sanger sample id through the remote server.
func (rc *RemoteClient) CountFindSamplesBySangerID(ctx context.Context, sangerID string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountFindSamplesBySangerID", []string{sangerID}, nil)
}

// CountFindSamplesByIDSampleLims counts the samples matching a LIMS sample id through the remote server.
func (rc *RemoteClient) CountFindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountFindSamplesByIDSampleLims", []string{idSampleLims}, nil)
}

// CountFindSamplesByAccessionNumber counts the samples matching an accession number through the remote server.
func (rc *RemoteClient) CountFindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountFindSamplesByAccessionNumber", []string{accessionNumber}, nil)
}

// CountFindSamplesBySupplierName counts the samples matching a supplier name through the remote server.
func (rc *RemoteClient) CountFindSamplesBySupplierName(ctx context.Context, supplierName string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountFindSamplesBySupplierName", []string{supplierName}, nil)
}

// CountFindSamplesByLibraryType counts the samples matching a library type through the remote server.
func (rc *RemoteClient) CountFindSamplesByLibraryType(ctx context.Context, libraryType string) (Count, error) {
	return remoteCall[Count](rc, ctx, "CountFindSamplesByLibraryType", []string{libraryType}, nil)
}

// Freshness reports per-table sync freshness through the remote server.
func (rc *RemoteClient) Freshness(ctx context.Context) (Freshness, error) {
	return remoteCall[Freshness](rc, ctx, "Freshness", nil, nil)
}

// Enrich returns an enrichment graph through the remote server.
func (rc *RemoteClient) Enrich(ctx context.Context, identifier string) (EnrichmentResult, error) {
	return remoteCall[EnrichmentResult](rc, ctx, "Enrich", []string{identifier}, nil)
}

// SampleDetail returns sample detail through the remote server.
func (rc *RemoteClient) SampleDetail(ctx context.Context, sangerName string) (SampleDetail, error) {
	return remoteCall[SampleDetail](rc, ctx, "SampleDetail", []string{sangerName}, nil)
}

// StudyDetail returns study detail through the remote server.
func (rc *RemoteClient) StudyDetail(ctx context.Context, studyLimsID string) (StudyDetail, error) {
	return remoteCall[StudyDetail](rc, ctx, "StudyDetail", []string{studyLimsID}, nil)
}

// RunDetail returns run detail through the remote server.
func (rc *RemoteClient) RunDetail(ctx context.Context, idRun string) (RunDetail, error) {
	return remoteCall[RunDetail](rc, ctx, "RunDetail", []string{idRun}, nil)
}

// LibraryDetail returns library detail through the remote server.
func (rc *RemoteClient) LibraryDetail(ctx context.Context, pipelineIDLims, studyLimsID string) (LibraryDetail, error) {
	return remoteCall[LibraryDetail](rc, ctx, "LibraryDetail", []string{pipelineIDLims, studyLimsID}, nil)
}

// Call is the body-only generic counterpart to the typed methods (ResolveSample,
// AllStudies, ...): instead of selecting an endpoint at compile time, it
// dispatches to the Registry entry whose Method name equals method. It is for
// callers that choose an endpoint dynamically by Registry Method name, such as
// a generic "call any endpoint" tool, without a hand-written switch.
//
// pathParams supplies the endpoint's path parameters in declaration order and
// query supplies any query parameters (e.g. limit/offset for paginated
// endpoints). The decoded typed result is returned as an any holding a pointer
// to the endpoint's NewResult type (for example *Match for ResolveSample or
// *[]Study for AllStudies); type-assert it to that pointer to read the value.
//
// Call surfaces the same errors as the typed methods, including an unknown or
// missing Registry method and a path-param arity mismatch. Use CallWithHeaders
// when response headers such as X-Total-Count or X-Next-Offset are needed.
func (rc *RemoteClient) Call(ctx context.Context, method string, pathParams []string, query url.Values) (any, error) {
	result, _, err := rc.CallWithHeaders(ctx, method, pathParams, query)

	return result, err
}

// CallWithHeaders is the header-aware generic counterpart to the typed methods.
// It dispatches to the Registry entry whose Method name equals method and returns
// the decoded typed result as an any holding a pointer to the endpoint's
// NewResult type, plus the HTTP response headers from the remote server.
//
// pathParams supplies the endpoint's path parameters in declaration order and
// query supplies any query parameters. CallWithHeaders surfaces the same errors
// as Call and the typed methods, including unknown Registry methods,
// path-param arity mismatches, and decoded remote error envelopes.
func (rc *RemoteClient) CallWithHeaders(ctx context.Context, method string, pathParams []string, query url.Values) (any, http.Header, error) {
	return rc.do(ctx, method, pathParams, query)
}

// do issues the request for method and returns the decoded typed result, the
// response headers, and any error. It is the single shared request path: the
// bare-slice/value methods (via remoteCall, which ignores the headers) and the
// typed Page[T] paged variants (via remoteCallPage, which reads X-Total-Count /
// X-Next-Offset from the returned header) both go through it, so the sizing
// headers are captured in one place.
func (rc *RemoteClient) do(ctx context.Context, method string, pathParams []string, query url.Values) (any, http.Header, error) {
	if rc == nil || rc.httpClient == nil {
		return nil, nil, fmt.Errorf("%w: nil remote client", ErrUpstreamImpaired)
	}

	entry, ok := rc.endpoints[method]
	if !ok {
		return nil, nil, fmt.Errorf("%w: registry entry missing for %s", ErrUpstreamImpaired, method)
	}

	requestURL, err := rc.requestURL(entry, pathParams, query)
	if err != nil {
		return nil, nil, err
	}

	request, err := http.NewRequestWithContext(ctx, entry.Verb, requestURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: build %s request: %w", ErrUpstreamImpaired, method, err)
	}

	if rc.token != "" {
		request.Header.Set("Authorization", "Bearer "+rc.token)
	}

	response, err := rc.httpClient.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s request failed: %w", ErrUpstreamImpaired, method, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		result, err := decodeRemoteResult(response, entry)

		return result, response.Header, err
	}

	return nil, response.Header, decodeRemoteError(response, entry)
}

func decodeRemoteResult(response *http.Response, entry Endpoint) (any, error) {
	result := entry.NewResult()
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return nil, fmt.Errorf("%w: decode %s response: %w", ErrUpstreamImpaired, entry.Method, err)
	}

	return result, nil
}

func decodeRemoteError(response *http.Response, entry Endpoint) error {
	var envelope httpErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("%w: remote %s returned %d without a valid error envelope", ErrUpstreamImpaired, entry.Method, response.StatusCode)
	}

	sentinel := sentinelForHTTPErrorCode(envelope.Code)
	if sentinel == nil {
		sentinel = ErrUpstreamImpaired
	}

	if errors.Is(sentinel, ErrCacheNeverSynced) && endpointResultIsSlice(entry) {
		sentinel = errors.Join(ErrCacheNeverSynced, ErrNotFound)
	}

	message := envelope.Message
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	if message == "" {
		message = fmt.Sprintf("remote %s returned %d", entry.Method, response.StatusCode)
	}

	return fmt.Errorf("%s: %w", message, sentinel)
}

func (rc *RemoteClient) requestURL(entry Endpoint, pathParams []string, query url.Values) (string, error) {
	remotePath, err := remoteEndpointPath(entry, pathParams)
	if err != nil {
		return "", err
	}

	requestURL := rc.baseURL + remotePath
	if encodedQuery := query.Encode(); encodedQuery != "" {
		requestURL += "?" + encodedQuery
	}

	return requestURL, nil
}

func remoteEndpointPath(entry Endpoint, pathParams []string) (string, error) {
	if len(pathParams) != len(entry.PathParams) {
		return "", fmt.Errorf("%w: %s expects %d path params, got %d", ErrUpstreamImpaired, entry.Method, len(entry.PathParams), len(pathParams))
	}

	remotePath := entry.Path
	for i, name := range entry.PathParams {
		marker := ":" + name
		if !strings.Contains(remotePath, marker) {
			return "", fmt.Errorf("%w: %s path is missing %s", ErrUpstreamImpaired, entry.Method, marker)
		}
		remotePath = strings.ReplaceAll(remotePath, marker, url.PathEscape(pathParams[i]))
	}

	return remotePath, nil
}

func remoteCall[T any](rc *RemoteClient, ctx context.Context, method string, pathParams []string, query url.Values) (T, error) {
	var zero T

	result, _, err := rc.do(ctx, method, pathParams, query)
	if err != nil {
		return zero, err
	}

	typed, ok := result.(*T)
	if !ok {
		return zero, fmt.Errorf("%w: registry result for %s has type %T", ErrUpstreamImpaired, method, result)
	}

	return *typed, nil
}

// remoteCallPage is the Page[T] counterpart to remoteCall: it issues the same
// paginated list request through the shared do path but additionally reads the
// X-Total-Count / X-Next-Offset response headers into Page.Total / Page.NextOffset,
// so a Go consumer learns how many rows match and where the next page starts from
// one request. Page.Items is the decoded body, identical to the bare-slice
// method's result for the same args (the body stays a bare JSON array). Adding a
// Page[T] variant for any paginated list is therefore a one-line wrapper over
// this single shared header-reading path.
func remoteCallPage[T any](rc *RemoteClient, ctx context.Context, method string, pathParams []string, query url.Values) (Page[T], error) {
	result, header, err := rc.do(ctx, method, pathParams, query)
	if err != nil {
		return Page[T]{}, err
	}

	typed, ok := result.(*[]T)
	if !ok {
		return Page[T]{}, fmt.Errorf("%w: registry result for %s has type %T", ErrUpstreamImpaired, method, result)
	}

	return Page[T]{
		Items:      *typed,
		Total:      remoteHeaderInt(header, "X-Total-Count", 0),
		NextOffset: remoteHeaderInt(header, "X-Next-Offset", -1),
	}, nil
}

// remoteHeaderInt reads name from header as a base-10 int, returning fallback
// when the header is absent or not an integer, so a server that omits the
// list-sizing headers yields a well-defined Page rather than an error.
func remoteHeaderInt(header http.Header, name string, fallback int) int {
	raw := header.Get(name)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

// RemoteConfig configures a RemoteClient.
type RemoteConfig struct {
	BaseURL  string
	Timeout  time.Duration
	Token    string
	CACert   string
	CacheTTL time.Duration
}

func remoteEndpointMap() map[string]Endpoint {
	endpoints := make(map[string]Endpoint, len(Registry))
	for _, entry := range Registry {
		endpoints[entry.Method] = entry
	}

	return endpoints
}

func normalizeRemoteBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: remote base URL is required", ErrUpstreamImpaired)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: parse remote base URL: %w", ErrUpstreamImpaired, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: remote base URL must include scheme and host", ErrUpstreamImpaired)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""

	return strings.TrimRight(parsed.String(), "/"), nil
}

func newRemoteHTTPClient(timeout time.Duration, caPath string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	} else {
		transport = transport.Clone()
	}

	if caPath != "" {
		tlsConfig, err := remoteTLSConfig(caPath)
		if err != nil {
			return nil, err
		}

		transport.TLSClientConfig = tlsConfig
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

func remoteTLSConfig(caPath string) (*tls.Config, error) {
	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("%w: read CA cert: %w", ErrUpstreamImpaired, err)
	}
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("%w: parse CA cert %s", ErrUpstreamImpaired, caPath)
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
	}, nil
}

func endpointResultIsSlice(entry Endpoint) bool {
	result := entry.NewResult()
	resultType := reflect.TypeOf(result)

	return resultType != nil && resultType.Kind() == reflect.Pointer && resultType.Elem().Kind() == reflect.Slice
}
