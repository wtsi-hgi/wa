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

package saga

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

const defaultPaginationPageSize = 100

const mlwhStudyFilterKey = "study_id"

// PaginatedResponse is the generic paginated response envelope used by SAGA list endpoints.
type PaginatedResponse[T any] struct {
	Items  []T  `json:"items"`
	Total  *int `json:"total"`
	Offset int  `json:"offset"`
	Limit  int  `json:"limit"`
}

// PageOptions controls paginated list requests.
type PageOptions struct {
	Page      int
	PageSize  int
	SortField string
	SortOrder string
	Filters   map[string]string
}

func (o PageOptions) queryValues() url.Values {
	values := url.Values{}

	if o.Page > 0 {
		values.Set("page", strconv.Itoa(o.Page))
	}

	if o.PageSize > 0 {
		values.Set("pageSize", strconv.Itoa(o.PageSize))
	}

	if o.SortField != "" {
		values.Set("sortField", o.SortField)
	}

	if o.SortOrder != "" {
		values.Set("sortOrder", o.SortOrder)
	}

	if len(o.Filters) > 0 {
		filterJSON, _ := json.Marshal(o.Filters)
		values.Set("filters", string(filterJSON))
	}

	if len(values) == 0 {
		return nil
	}

	return values
}

// Study describes an MLWH study returned by SAGA.
type Study struct {
	IDStudyTmp           int    `json:"id_study_tmp"`
	IDLims               string `json:"id_lims"`
	IDStudyLims          string `json:"id_study_lims"`
	Name                 string `json:"name"`
	FacultySponsor       string `json:"faculty_sponsor"`
	State                string `json:"state"`
	Abstract             string `json:"abstract"`
	Abbreviation         string `json:"abbreviation"`
	AccessionNumber      string `json:"accession_number"`
	Description          string `json:"description"`
	DataReleaseStrategy  string `json:"data_release_strategy"`
	StudyTitle           string `json:"study_title"`
	DataAccessGroup      string `json:"data_access_group"`
	HMDMCNumber          string `json:"hmdmc_number"`
	Programme            string `json:"programme"`
	Created              string `json:"created"`
	ReferenceGenome      string `json:"reference_genome"`
	EthicallyApproved    bool   `json:"ethically_approved"`
	StudyType            string `json:"study_type"`
	ContainsHumanDNA     bool   `json:"contains_human_dna"`
	ContaminatedHumanDNA bool   `json:"contaminated_human_dna"`
	StudyVisibility      string `json:"study_visibility"`
	EGADACAccession      string `json:"ega_dac_accession_number"`
	EGAPolicyAccession   string `json:"ega_policy_accession_number"`
	DataReleaseTiming    string `json:"data_release_timing"`
}

// MLWHSample describes an MLWH sample row returned by SAGA.
type MLWHSample struct {
	IDStudyLims          string `json:"id_study_lims"`
	IDSampleLims         string `json:"id_sample_lims"`
	SangerID             string `json:"sanger_id"`
	SampleName           string `json:"sample_name"`
	TaxonID              int    `json:"taxon_id"`
	CommonName           string `json:"common_name"`
	LibraryType          string `json:"library_type"`
	IDRun                int    `json:"id_run"`
	Lane                 int    `json:"lane"`
	TagIndex             int    `json:"tag_index"`
	IRODSPath            string `json:"irods_path"`
	StudyAccessionNumber string `json:"study_accession_number"`
	AccessionNumber      string `json:"accession_number"`
}

// FacultySponsor describes an MLWH faculty sponsor returned by SAGA.
type FacultySponsor struct {
	Name string `json:"name"`
}

// Programme describes an MLWH programme returned by SAGA.
type Programme struct {
	Name string `json:"name"`
}

// DataReleaseStrategy describes an MLWH data release strategy returned by SAGA.
type DataReleaseStrategy struct {
	Name string `json:"name"`
}

// MLWHClient provides access to MLWH integration endpoints.
type MLWHClient struct {
	client *Client
}

// ListStudies returns one page of MLWH studies.
func (m *MLWHClient) ListStudies(
	ctx context.Context,
	opts PageOptions,
) (*PaginatedResponse[Study], error) {
	body, err := m.client.doGet(ctx, "/integrations/mlwh/studies", opts.queryValues())
	if err != nil {
		return nil, err
	}

	response := &PaginatedResponse[Study]{}
	if err := json.Unmarshal(body, response); err != nil {
		return nil, err
	}

	if response.Items == nil {
		response.Items = []Study{}
	}

	return response, nil
}

// AllStudies returns all MLWH studies across every available page.
func (m *MLWHClient) AllStudies(ctx context.Context) ([]Study, error) {
	return collectAllPages(ctx, m.ListStudies)
}

func collectAllPages[T any](
	ctx context.Context,
	fetchPage func(context.Context, PageOptions) (*PaginatedResponse[T], error),
) ([]T, error) {
	options := PageOptions{Page: 1, PageSize: defaultPaginationPageSize}
	items := make([]T, 0)

	for {
		response, err := fetchPage(ctx, options)
		if err != nil {
			return items, err
		}

		items = append(items, response.Items...)

		if len(response.Items) == 0 {
			return items, nil
		}

		if response.Total != nil && len(items) >= *response.Total {
			return items, nil
		}

		options.Page++
	}
}

// ListSamples returns one page of MLWH samples.
func (m *MLWHClient) ListSamples(
	ctx context.Context,
	opts PageOptions,
) (*PaginatedResponse[MLWHSample], error) {
	body, err := m.client.doGet(ctx, "/integrations/mlwh/samples", opts.queryValues())
	if err != nil {
		return nil, err
	}

	response := &PaginatedResponse[MLWHSample]{}
	if err := json.Unmarshal(body, response); err != nil {
		return nil, err
	}

	if response.Items == nil {
		response.Items = []MLWHSample{}
	}

	return response, nil
}

// AllSamples returns all MLWH sample rows across every available page.
func (m *MLWHClient) AllSamples(ctx context.Context) ([]MLWHSample, error) {
	return collectAllPages(ctx, m.ListSamples)
}

// AllSamplesForStudy returns all MLWH sample rows for one study across every available page.
func (m *MLWHClient) AllSamplesForStudy(ctx context.Context, studyID string) ([]MLWHSample, error) {
	return collectAllPages(ctx, func(ctx context.Context, opts PageOptions) (*PaginatedResponse[MLWHSample], error) {
		opts.Filters = map[string]string{mlwhStudyFilterKey: studyID}

		return m.ListSamples(ctx, opts)
	})
}

// ListFacultySponsors returns one page of MLWH faculty sponsors.
func (m *MLWHClient) ListFacultySponsors(
	ctx context.Context,
	opts PageOptions,
) (*PaginatedResponse[FacultySponsor], error) {
	body, err := m.client.doGet(ctx, "/integrations/mlwh/faculty_sponsors", opts.queryValues())
	if err != nil {
		return nil, err
	}

	response := &PaginatedResponse[FacultySponsor]{}
	if err := json.Unmarshal(body, response); err != nil {
		return nil, err
	}

	if response.Items == nil {
		response.Items = []FacultySponsor{}
	}

	return response, nil
}

// AllFacultySponsors returns all MLWH faculty sponsors across every available page.
func (m *MLWHClient) AllFacultySponsors(ctx context.Context) ([]FacultySponsor, error) {
	return collectAllPages(ctx, m.ListFacultySponsors)
}

// ListProgrammes returns all MLWH programmes from the array response endpoint.
func (m *MLWHClient) ListProgrammes(ctx context.Context) ([]Programme, error) {
	body, err := m.client.doGet(ctx, "/integrations/mlwh/programmes", nil)
	if err != nil {
		return nil, err
	}

	programmes := make([]Programme, 0)
	if err := json.Unmarshal(body, &programmes); err != nil {
		return nil, err
	}

	return programmes, nil
}

// ListDataReleaseStrategies returns all MLWH data release strategies from the array response endpoint.
func (m *MLWHClient) ListDataReleaseStrategies(ctx context.Context) ([]DataReleaseStrategy, error) {
	body, err := m.client.doGet(ctx, "/integrations/mlwh/data_release_strategies", nil)
	if err != nil {
		return nil, err
	}

	strategies := make([]DataReleaseStrategy, 0)
	if err := json.Unmarshal(body, &strategies); err != nil {
		return nil, err
	}

	return strategies, nil
}

// GetStudy returns a single MLWH study by study ID.
func (m *MLWHClient) GetStudy(ctx context.Context, studyID string) (*Study, error) {
	body, err := m.client.doGet(ctx, fmt.Sprintf("/integrations/mlwh/studies/%s", url.PathEscape(studyID)), nil)
	if err != nil {
		return nil, err
	}

	study := &Study{}
	if err := json.Unmarshal(body, study); err != nil {
		return nil, err
	}

	return study, nil
}

// MLWH returns a client for MLWH integration endpoints.
func (c *Client) MLWH() *MLWHClient {
	return &MLWHClient{client: c}
}
