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
)

// IRODSSample describes an iRODS sample returned by SAGA.
type IRODSSample struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Source   string         `json:"source"`
	SourceID string         `json:"source_id"`
	Data     map[string]any `json:"data"`
	Curated  map[string]any `json:"curated"`
	Parent   *int           `json:"parent"`
}

// IRODSMetadata describes a single iRODS metadata entry.
type IRODSMetadata struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// IRODSFile describes an iRODS file or collection returned by SAGA.
type IRODSFile struct {
	ID         int             `json:"id"`
	Collection string          `json:"collection"`
	Metadata   []IRODSMetadata `json:"metadata"`
}

// IRODSAnalysisType describes a named iRODS analysis type returned by SAGA.
type IRODSAnalysisType struct {
	Name string `json:"name"`
}

// IRODSClient provides access to iRODS integration endpoints.
type IRODSClient struct {
	client *Client
}

// ListSamples returns one page of iRODS samples.
func (i *IRODSClient) ListSamples(
	ctx context.Context,
	opts PageOptions,
) (*PaginatedResponse[IRODSSample], error) {
	body, err := i.client.doGet(ctx, "/integrations/irods/samples", opts.queryValues())
	if err != nil {
		return nil, err
	}

	response := &PaginatedResponse[IRODSSample]{}
	if err := json.Unmarshal(body, response); err != nil {
		return nil, err
	}

	if response.Items == nil {
		response.Items = []IRODSSample{}
	}

	return response, nil
}

// AllSamples returns all iRODS samples across every available page.
func (i *IRODSClient) AllSamples(ctx context.Context) ([]IRODSSample, error) {
	return collectAllPages(ctx, i.ListSamples)
}

// Ping checks whether the iRODS integration root endpoint is reachable.
func (i *IRODSClient) Ping(ctx context.Context) error {
	_, err := i.client.doGet(ctx, "/integrations/irods/", nil)

	return err
}

// GetSampleFiles returns the iRODS files associated with a Sanger sample ID.
func (i *IRODSClient) GetSampleFiles(ctx context.Context, sangerID string) ([]IRODSFile, error) {
	body, err := i.client.doGet(ctx,
		fmt.Sprintf("/integrations/irods/samples/%s", url.PathEscape(sangerID)), nil)
	if err != nil {
		return nil, err
	}

	response := struct {
		Items []IRODSFile `json:"items"`
	}{}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	if response.Items == nil {
		response.Items = []IRODSFile{}
	}

	return response.Items, nil
}

// GetWebSummary returns the raw web summary response for an iRODS collection.
func (i *IRODSClient) GetWebSummary(ctx context.Context, collection string) ([]byte, error) {
	return i.client.doGet(ctx,
		fmt.Sprintf("/integrations/irods/web-summary/%s", url.PathEscape(collection)), nil)
}

// ListAnalysisTypes returns all iRODS analysis types from the array response endpoint.
func (i *IRODSClient) ListAnalysisTypes(ctx context.Context) ([]IRODSAnalysisType, error) {
	body, err := i.client.doGet(ctx, "/integrations/irods/analysis-types", nil)
	if err != nil {
		return nil, err
	}

	types := make([]IRODSAnalysisType, 0)
	if err := json.Unmarshal(body, &types); err != nil {
		return nil, err
	}

	return types, nil
}

// IRODS returns a client for iRODS integration endpoints.
func (c *Client) IRODS() *IRODSClient {
	return &IRODSClient{client: c}
}
