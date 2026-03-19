# Phase 5: Projects Endpoints

Ref: [spec.md](spec.md) sections F1, F2, F3, F4, F5, F6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 5.1: F1 - List projects

spec.md section: F1

Implement `ProjectsClient.List(ctx) ([]Project, error)` in
`saga/projects.go`. Calls `GET /projects/`. Test in
`saga/projects_test.go` covering the 1 acceptance test from F1.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after 5.1 is reviewed)

#### Item 5.2: F2 - Add project [parallel with 5.3, 5.4, 5.5, 5.6]

spec.md section: F2

Implement `ProjectsClient.Add(ctx, name) (*Project, error)` in
`saga/projects.go`. Calls `POST /projects/` and invalidates the
projects list cache. Test in `saga/projects_test.go` covering all
2 acceptance tests from F2.

- [ ] implemented
- [ ] reviewed

#### Item 5.3: F3 - Get project [parallel with 5.2, 5.4, 5.5, 5.6]

spec.md section: F3

Implement `ProjectsClient.Get(ctx, projectID) (*Project, error)`
in `saga/projects.go`. Calls `GET /projects/{id}`. Test in
`saga/projects_test.go` covering all 2 acceptance tests from F3.

- [ ] implemented
- [ ] reviewed

#### Item 5.4: F4 - Project samples [parallel with 5.2, 5.3, 5.5, 5.6]

spec.md section: F4

Implement `ProjectsClient.ListSamples`, `AddSample`, and
`RemoveSample` in `saga/projects.go`. POST/DELETE invalidate the
project samples cache. Test in `saga/projects_test.go` covering
all 3 acceptance tests from F4.

- [ ] implemented
- [ ] reviewed

#### Item 5.5: F5 - Project studies [parallel with 5.2, 5.3, 5.4, 5.6]

spec.md section: F5

Implement `ProjectsClient.ListStudies`, `AddStudy`, and
`RemoveStudy` in `saga/projects.go`. POST/DELETE invalidate the
project studies cache. Test in `saga/projects_test.go` covering
all 3 acceptance tests from F5.

- [ ] implemented
- [ ] reviewed

#### Item 5.6: F6 - Project users [parallel with 5.2, 5.3, 5.4, 5.5]

spec.md section: F6

Implement `ProjectsClient.ListUsers`, `AddUser`, and `RemoveUser`
in `saga/projects.go`. POST/DELETE invalidate the project users
cache. Test in `saga/projects_test.go` covering all 3 acceptance
tests from F6.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
