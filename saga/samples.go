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
)

// SagaSample describes a Saga sample returned by the internal samples endpoints.
type SagaSample struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Source   string         `json:"source"`
	SourceID string         `json:"source_id"`
	Data     map[string]any `json:"data"`
	Curated  map[string]any `json:"curated"`
	Parent   *int           `json:"parent"`
}

// SagaStudy describes a Saga study returned by the internal samples endpoints.
type SagaStudy struct {
	ID          int    `json:"id"`
	IDStudyLims string `json:"id_study_lims"`
	Name        string `json:"name"`
}

// CreateSampleRequest is the POST body used to create a Saga sample.
type CreateSampleRequest struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
}

// SamplesClient provides access to Saga samples endpoints.
type SamplesClient struct {
	client *Client
}

// List returns one page of Saga samples.
func (s *SamplesClient) List(
	ctx context.Context,
	opts PageOptions,
) (*PaginatedResponse[SagaSample], error) {
	body, err := s.client.doGet(ctx, "/samples/", opts.queryValues())
	if err != nil {
		return nil, err
	}

	response := &PaginatedResponse[SagaSample]{}
	if err := json.Unmarshal(body, response); err != nil {
		return nil, err
	}

	if response.Items == nil {
		response.Items = []SagaSample{}
	}

	return response, nil
}

// All returns all Saga samples across every available page.
func (s *SamplesClient) All(ctx context.Context) ([]SagaSample, error) {
	return collectAllPages(ctx, s.List)
}

// Create creates a Saga sample from an upstream source identifier.
func (s *SamplesClient) Create(
	ctx context.Context,
	source string,
	sourceID string,
) (*SagaSample, error) {
	body, err := s.client.doPost(ctx, "/samples/", CreateSampleRequest{
		Source:   source,
		SourceID: sourceID,
	})
	if err != nil {
		return nil, err
	}

	sample := &SagaSample{}
	if err := json.Unmarshal(body, sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// GetBySource returns a Saga sample for the given source and source ID.
func (s *SamplesClient) GetBySource(
	ctx context.Context,
	source string,
	sourceID string,
) (*SagaSample, error) {
	body, err := s.client.doGet(ctx, fmt.Sprintf("/samples/%s/%s", source, sourceID), nil)
	if err != nil {
		return nil, err
	}

	sample := &SagaSample{}
	if err := json.Unmarshal(body, sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// GetStudySamples returns the Saga samples for the given source study identifier.
func (s *SamplesClient) GetStudySamples(
	ctx context.Context,
	source string,
	studySourceID string,
) ([]SagaSample, error) {
	body, err := s.client.doGet(
		ctx,
		fmt.Sprintf("/samples/%s/studies/%s", source, studySourceID),
		nil,
	)
	if err != nil {
		return nil, err
	}

	samples := []SagaSample{}
	if err := json.Unmarshal(body, &samples); err != nil {
		return nil, err
	}

	return samples, nil
}

// ListStudies returns the Saga studies available from the internal samples endpoints.
func (s *SamplesClient) ListStudies(ctx context.Context) ([]SagaStudy, error) {
	body, err := s.client.doGet(ctx, "/studies/", nil)
	if err != nil {
		return nil, err
	}

	studies := []SagaStudy{}
	if err := json.Unmarshal(body, &studies); err == nil {
		return studies, nil
	}

	decodeErr := json.Unmarshal(body, &studies)

	var namespaceRoot struct {
		Studies string `json:"studies"`
	}

	if err := json.Unmarshal(body, &namespaceRoot); err != nil || namespaceRoot.Studies == "" {
		return nil, decodeErr
	}

	mlwhStudies, err := s.client.MLWH().AllStudies(ctx)
	if err != nil {
		return nil, err
	}

	studies = make([]SagaStudy, 0, len(mlwhStudies))
	for _, study := range mlwhStudies {
		studies = append(studies, SagaStudy{
			ID:          study.IDStudyTmp,
			IDStudyLims: study.IDStudyLims,
			Name:        study.Name,
		})
	}

	return studies, nil
}

// Samples returns a client for Saga samples endpoints.
func (c *Client) Samples() *SamplesClient {
	return &SamplesClient{client: c}
}
