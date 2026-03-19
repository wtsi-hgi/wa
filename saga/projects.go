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

// Project describes a SAGA project returned by the projects endpoint.
type Project struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ProjectSample describes a SAGA sample associated with a project.
type ProjectSample struct {
	ID       int    `json:"id"`
	SangerID string `json:"sanger_id"`
}

// ProjectStudy describes a SAGA study associated with a project.
type ProjectStudy struct {
	ID          int    `json:"id"`
	IDStudyLims string `json:"id_study_lims"`
}

// ProjectUser describes a SAGA user associated with a project.
type ProjectUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

// ProjectsClient provides access to SAGA project endpoints.
type ProjectsClient struct {
	client *Client
}

// List returns the SAGA projects visible to the authenticated caller.
func (p *ProjectsClient) List(ctx context.Context) ([]Project, error) {
	body, err := p.client.doGet(ctx, "/projects/", nil)
	if err != nil {
		return nil, err
	}

	projects := []Project{}
	if err := json.Unmarshal(body, &projects); err != nil {
		return nil, err
	}

	return projects, nil
}

// Add creates a new SAGA project with the provided name.
func (p *ProjectsClient) Add(ctx context.Context, name string) (*Project, error) {
	body, err := p.client.doPost(ctx, "/projects/", struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return nil, err
	}

	project := &Project{}
	if err := json.Unmarshal(body, project); err != nil {
		return nil, err
	}

	return project, nil
}

// Get returns a SAGA project by ID.
func (p *ProjectsClient) Get(ctx context.Context, projectID int) (*Project, error) {
	body, err := p.client.doGet(ctx, fmt.Sprintf("/projects/%d", projectID), nil)
	if err != nil {
		return nil, err
	}

	project := &Project{}
	if err := json.Unmarshal(body, project); err != nil {
		return nil, err
	}

	return project, nil
}

// ListSamples returns the samples associated with a SAGA project.
func (p *ProjectsClient) ListSamples(ctx context.Context, projectID int) ([]ProjectSample, error) {
	body, err := p.client.doGet(ctx, fmt.Sprintf("/projects/%d/samples/", projectID), nil)
	if err != nil {
		return nil, err
	}

	samples := []ProjectSample{}
	if err := json.Unmarshal(body, &samples); err != nil {
		return nil, err
	}

	return samples, nil
}

// AddSample adds a sample to a SAGA project.
func (p *ProjectsClient) AddSample(ctx context.Context, projectID int, sampleID string) (*ProjectSample, error) {
	body, err := p.client.doPost(ctx, fmt.Sprintf("/projects/%d/samples/", projectID), struct {
		SangerID string `json:"sanger_id"`
	}{SangerID: sampleID})
	if err != nil {
		return nil, err
	}

	sample := &ProjectSample{}
	if err := json.Unmarshal(body, sample); err != nil {
		return nil, err
	}

	return sample, nil
}

// RemoveSample removes a sample from a SAGA project.
func (p *ProjectsClient) RemoveSample(ctx context.Context, projectID int, sampleID int) error {
	return p.client.doDelete(ctx, fmt.Sprintf("/projects/%d/samples/%d", projectID, sampleID))
}

// ListStudies returns the studies associated with a SAGA project.
func (p *ProjectsClient) ListStudies(ctx context.Context, projectID int) ([]ProjectStudy, error) {
	body, err := p.client.doGet(ctx, fmt.Sprintf("/projects/%d/studies/", projectID), nil)
	if err != nil {
		return nil, err
	}

	studies := []ProjectStudy{}
	if err := json.Unmarshal(body, &studies); err != nil {
		return nil, err
	}

	return studies, nil
}

// AddStudy adds a study to a SAGA project.
func (p *ProjectsClient) AddStudy(ctx context.Context, projectID int, studyID string) (*ProjectStudy, error) {
	body, err := p.client.doPost(ctx, fmt.Sprintf("/projects/%d/studies/", projectID), struct {
		IDStudyLims string `json:"id_study_lims"`
	}{IDStudyLims: studyID})
	if err != nil {
		return nil, err
	}

	study := &ProjectStudy{}
	if err := json.Unmarshal(body, study); err != nil {
		return nil, err
	}

	return study, nil
}

// RemoveStudy removes a study from a SAGA project.
func (p *ProjectsClient) RemoveStudy(ctx context.Context, projectID int, studyID int) error {
	return p.client.doDelete(ctx, fmt.Sprintf("/projects/%d/studies/%d", projectID, studyID))
}

// ListUsers returns the users associated with a SAGA project.
func (p *ProjectsClient) ListUsers(ctx context.Context, projectID int) ([]ProjectUser, error) {
	body, err := p.client.doGet(ctx, fmt.Sprintf("/projects/%d/users/", projectID), nil)
	if err != nil {
		return nil, err
	}

	users := []ProjectUser{}
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, err
	}

	return users, nil
}

// AddUser adds a user to a SAGA project.
func (p *ProjectsClient) AddUser(ctx context.Context, projectID int, username string) (*ProjectUser, error) {
	body, err := p.client.doPost(ctx, fmt.Sprintf("/projects/%d/users/", projectID), struct {
		Username string `json:"username"`
	}{Username: username})
	if err != nil {
		return nil, err
	}

	user := &ProjectUser{}
	if err := json.Unmarshal(body, user); err != nil {
		return nil, err
	}

	return user, nil
}

// RemoveUser removes a user from a SAGA project.
func (p *ProjectsClient) RemoveUser(ctx context.Context, projectID int, userID int) error {
	return p.client.doDelete(ctx, fmt.Sprintf("/projects/%d/users/%d", projectID, userID))
}

// Projects returns a client for SAGA project endpoints.
func (c *Client) Projects() *ProjectsClient {
	return &ProjectsClient{client: c}
}
