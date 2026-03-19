# Phase 8: Integration Tests

Ref: [spec.md](spec.md) section I1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 8.1: I1 - Real API integration

spec.md section: I1

Implement integration tests in `saga/integration_test.go`. Tests
skip unless `SAGA_TEST_API_TOKEN` env var is set. Exercise real API
endpoints: `Ping`, `Version`, `MLWH().GetStudy("6568")`,
`IRODS().GetSampleFiles("WTSI_wEMB10524782")`,
`SampleAllMetadata("WTSI_wEMB10524782")`,
`StudyAllSamples("3361")`, `StudyIRODSFiles("6568")`,
`SampleIRODSFiles("WTSI_wEMB10524782", nil)`, and
`Samples().ListStudies()`. Depends on all prior phases. Test
file covers all 9 acceptance tests from I1.

- [ ] implemented
- [ ] reviewed
