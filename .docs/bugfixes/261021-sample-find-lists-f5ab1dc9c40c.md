# Sample find endpoints refuse shared identifiers

- Branch: samplefind-e06a25d4
- Base: origin/develop @ fa77143
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] `GET /find/sample/supplier-name/:id` (and the other FindSamplesBy\*
      lookups: sanger-id, lims-id, accession, library-type) return 409
      ambiguous when more than one sample matches, though the endpoint
      registry and MLWH_API_REFERENCE.md say e.g. "returns the samples whose
      supplier name exactly matches". A supplier name is legitimately shared
      by several samples (e.g. PD42478a: 4 samples; PD42487a: 2).
    - Red command:
      `go test ./mlwh -run TestFindSampleEndpointsListEverySampleSharingTheValue -count=1`
      -> `Expected: 200  Actual: 409` for each shared value.
    - Cause: the find queries used `LIMIT 2` and `sampleMatchResult` turned a
      second row into `ErrAmbiguous`. The cache relies on no upstream
      uniqueness beyond its primary keys, so any of the five fields can name
      several SQSCP samples.
    - Fixed in `mlwh/hierarchy.go`: the five finds share one paged
      `sampleFinder` (list SQL ordered by Sanger name, `LIMIT ? OFFSET ?`, plus
      the existing `/count` SQL). The routes are paginated like other sample
      lists (fetch-all default, `X-Total-Count` from `/count`). An empty page
      (past the last match, or `limit=0`) of a held value is `[]`; a value
      nobody holds stays 404. `Queryer`,
      `RemoteClient`, the server handlers and the registry take
      `limit, offset`. MLWH API 1.13.0; `MLWH_API_REFERENCE.md` regenerated.
      Enrichment, which needs one sample, uses `findUniqueSample` and still
      reports ambiguity (`TestFindUniqueSampleReportsSharedValuesAsAmbiguous`). `resolve/*` and export parents are unchanged.
    - `wa mlwh info`: `refreshSampleFanOut` picks the sample itself from the
      list (it took the first row) and skips an older server's ambiguous
      answer; the candidate list now includes samples sharing a Sanger
      sample id, LIMS id or accession
      (`TestMLWHInfoNonInteractiveSampleList`, accession `TWIN-ACC`).
- [x] Where an ambiguity error is still produced, the message says
      `"PD42487a" ambiguous between 4018299 and 4032054`: internal
      id_sample_tmp values, unlabelled, and only the first two. An LLM looked
      them up as LIMS ids and got unrelated samples. List the candidates by
      Sanger sample name, with the count, up to a cap.
    - Red command:
      `go test ./mlwh -run TestAmbiguityErrorsNameCandidateSamplesAndCount -count=1`
      -> `"PD42487a" matches samples 4018299 and 4032054` lacks
      `"PD42487a" matches 2 samples: 5534STDY8112792, 5534STDY7888714`; the
      study resolver said `matches studies 7001 and 7002` with no count.
    - Fixed: `ambiguousError` (`mlwh/hierarchy.go`) formats
      `"<raw>" matches N samples: name, ... and M more` (cap 10). Sample and
      study resolver lookups fetch `LIMIT 11` and count the rest only when the
      cap overflows; the cross-column cascade lists its candidates by name;
      studies are listed by LIMS study id. Code `ambiguous` and status 409 are
      unchanged. `TestResolveSampleCrossColumnCascadeReturnsAmbiguous` asserted
      the internal ids, which is the reported defect, so it now asserts the
      names.
- [x] The run-dev test fixture lacks donor_samples rows, so donor-id lookups
      return not_found on the fixture.
    - Red command:
      `go test ./cmd -run TestRunDevScriptUsesEphemeralMLWHCacheInTestMode -count=1`
      -> `GET /resolve/sample/DONOR7607` `Expected: 200  Actual: 404`.
    - Cause: `seed_test_mlwh_cache` in `run-dev.sh` (the `--mode test` cache
      that `make test` and verify-wa use; `make dev FIXTURES=1` seeds only
      results) never filled `donor_samples`, which sync derives from
      `sample_mirror`.
    - Fixed by deriving `donor_samples` from `sample_mirror` in the seed, as
      sync's cold-load rebuild does.
