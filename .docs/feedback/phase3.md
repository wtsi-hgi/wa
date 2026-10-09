# Phase 3: OpenAPI, version, and remote client

Ref: [spec.md](spec.md) sections B7, C1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Batch 1 (parallel)

#### Item 3.1: B7 - OpenAPI document and API version [parallel with 3.2]

spec.md section: B7

In `mlwh/openapi.go`, set `APIVersion = "1.9.0"`, replace
`info.description` with the exact text in spec.md B7, and add
`addOpenAPIFeedbackPath(paths, collector)`, called from `OpenAPIDocument`
next to `addOpenAPIHealthPath`, documenting only `post` on `/feedback`.
Set `additionalProperties: true` on the `FeedbackSubmission` component only
and add the `category` enum.
Update `TestOpenAPIErrorEnvelopeC2` to nine codes and
`TestAPIVersionIsThePhase1DocumentationPatch` to the new version in
`mlwh/openapi_test.go`. Regenerate `MLWH_API_REFERENCE.md` with
`WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference`; only its
version line changes. Depends on phase 2. Covering all 8 acceptance tests
from B7.

- [x] implemented
- [x] reviewed

#### Item 3.2: C1 - RemoteClient.SubmitFeedback [parallel with 3.1]

spec.md section: C1

Create `mlwh/remote_feedback.go` with `RemoteClient.SubmitFeedback` and the
unexported `submitFeedbackEndpoint` (with `NewResult` set, not added to
`Registry`). Non-2xx bodies are read once, re-wrapped, and passed to
`decodeRemoteError`, then joined with the sentinel from the status table in
spec.md C1. A 400 maps to `ErrFeedbackInvalid` only with a `bad_request`
envelope. Tests go in `mlwh/remote_feedback_test.go`, including the real
`NewServer(..., WithFeedback(...))` round trip. Depends on phase 2. Covering all
11 acceptance tests from C1.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
