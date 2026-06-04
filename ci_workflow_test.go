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

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/smartystreets/goconvey/convey"
)

type workflowPullRequestForTest struct {
	Branches []string `yaml:"branches"`
}

type workflowTriggersForTest struct {
	PullRequest workflowPullRequestForTest `yaml:"pull_request"`
}

type workflowStepForTest struct {
	Run string `yaml:"run"`
}

type workflowJobForTest struct {
	Steps []workflowStepForTest `yaml:"steps"`
}

func runCommandsForTest(job workflowJobForTest) []string {
	commands := make([]string, 0, len(job.Steps))
	for _, step := range job.Steps {
		if step.Run == "" {
			continue
		}

		commands = append(commands, step.Run)
	}

	return commands
}

type workflowForTest struct {
	On   workflowTriggersForTest       `yaml:"on"`
	Jobs map[string]workflowJobForTest `yaml:"jobs"`
}

func TestPullRequestCIWorkflow(t *testing.T) {
	convey.Convey("pull requests against develop run lint and test as separate jobs", t, func() {
		contents, err := os.ReadFile(filepath.Join(".github", "workflows", "ci.yml"))
		convey.So(err, convey.ShouldBeNil)

		var workflow workflowForTest
		err = yaml.Unmarshal(contents, &workflow)
		convey.So(err, convey.ShouldBeNil)

		convey.So(workflow.On.PullRequest.Branches, convey.ShouldResemble, []string{"develop"})

		lintJob, hasLintJob := workflow.Jobs["lint"]
		testJob, hasTestJob := workflow.Jobs["test"]
		convey.So(hasLintJob, convey.ShouldBeTrue)
		convey.So(hasTestJob, convey.ShouldBeTrue)

		lintCommands := runCommandsForTest(lintJob)
		testCommands := runCommandsForTest(testJob)
		convey.So(slices.Contains(lintCommands, "make lint"), convey.ShouldBeTrue)
		convey.So(slices.Contains(lintCommands, "make test"), convey.ShouldBeFalse)
		convey.So(slices.Contains(testCommands, "make test"), convey.ShouldBeTrue)
		convey.So(slices.Contains(testCommands, "make lint"), convey.ShouldBeFalse)
	})
}
