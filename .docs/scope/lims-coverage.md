# LIMS coverage

wa mirrors and serves only Sequencescape studies and samples, the MLWH rows
with `id_lims = 'SQSCP'`. Every source query that reads `study` or `sample`
keeps only `SQSCP` rows, and every cache read of `study_mirror` filters on
`id_lims = 'SQSCP'` again. Rows from any other LIMS, including the GCLP
LIMSes, Traction and Sapio, are out of scope. wa could support them if users
ask for that data; [Supporting another LIMS](#supporting-another-lims) lists
what the change involves.

## Origin

The filter is inherited, not argued. The cellgeni saga backend filtered
studies and samples on `id_lims="SQSCP"` from its first MLWH queries
(`backend/saga/views/mlwh.py`, saga commit `2002ee3`, 2025-03-31). wa's
`.docs/mlwh/prompt.md` (commit `5b274b1`, 2026-05-11, which replaced saga)
adopted it as "scoped to `id_lims = 'SQSCP'` everywhere unless explicitly
broadened". Neither repo records a reason. Keeping it is now a deliberate
scope decision: no user has asked for another LIMS, and what is excluded today
is small and mostly a decade old.

## What is excluded

Counts from the live MLWH on 2026-10-21.

| LIMS       | Studies | Samples | Last updated | Sequencing data                  | iRODS rows |
| ---------- | ------- | ------- | ------------ | -------------------------------- | ---------- |
| `C_GCLP`   | 11      | 8374    | 2016-08-01   | Illumina, 393 runs               | 150        |
| `C_GCLPVL` | 2       | 494     | 2016-02-26   | Illumina, 18 runs                | 0          |
| `Traction` | 0       | 265     | 2026-08-05   | PacBio, in Sequencescape studies | 502        |
| Sapio      | 0       | 0       | -            | -                                | -          |

- `C_GCLP` is the GCLP clinical pipeline: EORTC and HMRN tumour panels, a
  prostate pilot, a QA study and a Fluidigm boost (`HIL101`, `HIL353`,
  `SH1154` to `SH9352`). None has a faculty sponsor or `created`, and each
  `study_title` is `N/A`. Its samples are human, apart from 8 dummies and 75
  Coriell or 1000 Genomes controls. 10 of the 11 studies have `iseq_flowcell`
  rows (10938 rows, 5008 samples, runs 16610 to 21178, flowcells last updated
  2016-10-27) with 10548 product metrics rows. 150 products have iRODS
  locations under `/seq/19428` to `/seq/19465`, registered on 2022-08-16.
- `C_GCLPVL` is GCLP system validation (`SV1453`, `SV2454`): 494 human
  samples, 382 `iseq_flowcell` rows over 183 samples, runs 16075 to 16196,
  all from 2015-06-17, and no iRODS locations.
- GCLP studies hold only GCLP samples, and they have no PacBio, Element,
  Ultima, ONT or BMAP rows.
- `Traction` has samples but no studies. They are Darwin Tree of Life and
  BIOSCAN species plus 97 human samples, created 2024-12-05 to 2026-03-03. 240
  of the 265 appear in `pac_bio_run` under four Sequencescape studies: 5901
  (DTOL), 6798 (BIOSCAN), 7406 (HG_TP10476_JAGUAR) and 8010 (HG_WC11142), in
  runs `TRACTION-RUN-1406` to `TRACTION-RUN-2629`. Those rows have 508
  `pac_bio_product_metrics` rows and 502 iRODS locations under
  `/seq/pacbio/`, for 220 samples, the latest registered on 2026-10-08.
- Sapio studies may share a uuid with a Sequencescape study from the
  [2026-10-22 release](../upstream/2026-10-22-uwh.md) and are excluded the
  same way. None exist yet.

No excluded study or sample shares `id_study_lims`, `id_sample_lims`, name or
`sanger_sample_id` with a Sequencescape row today.

### Traction samples in Sequencescape studies

Sample sync keeps only `SQSCP` samples, but the product and iRODS source
queries filter on the study's LIMS alone. The PacBio product metrics and iRODS
locations of Traction samples in studies 5901, 6798, 7406 and 8010 therefore
reach the cache with an `id_sample_tmp` that has no `sample_mirror` row. From
the read SQL, not checked against a running cache:

- Study iRODS listings and counts, latest data and the iRODS export
  `LEFT JOIN sample_mirror`, so they include these paths with empty sample
  name and identifiers.
- Reads that `INNER JOIN sample_mirror`, such as sample lookups and per-sample
  listings, drop them.

This is intended: the data belongs to Sequencescape studies, so study-level
answers include it, while the samples themselves stay out of scope. Do not
filter these rows out without a user request.

## Supporting another LIMS

If users ask for another LIMS, the change touches:

- Sync: every `id_lims = 'SQSCP'` filter on `study` and `sample` in
  `mlwh/sync.go` and `mlwh/sync_platform_coverage.go`, and `syncStudyTable`'s
  Go filter. Decide each table separately, including whether products of a
  non-SQSCP sample in an SQSCP study stay as above.
- Lookups and uniqueness: every `study_mirror` and `sample_mirror` read,
  uuid lookups (`ResolveStudy`, `ClassifyIdentifier`) and cache keys. From the
  2026-10-22 release a study uuid is unique only per `id_lims`, as
  `id_study_lims` and `id_sample_lims` already are, so a lookup by any of them
  becomes ambiguous and must take or return the LIMS.
- Output: show `id_lims` on studies and samples and add a LIMS filter to list,
  count and export endpoints.
- API: bump the API version for the changed contract and update the MCP tool
  descriptions and `.docs/mcp/glossary.md`, which state the `SQSCP` scope.
- Tests: `mlwh/upstream_schema_change_test.go` pins the exclusion of a Sapio
  study that shares a uuid and `id_study_lims`. Those tests must change to
  pin the new behaviour.

## Re-running the investigation

Read-only, against the MLWH replica with the `WA_MLWH_DSN` user, and with
`SET SESSION MAX_EXECUTION_TIME=120000` first. Pass the password in
`MYSQL_PWD`.

```sql
SELECT id_lims, COUNT(*), MAX(last_updated) FROM study GROUP BY id_lims;
SELECT id_lims, COUNT(*), MAX(last_updated) FROM sample GROUP BY id_lims;

-- Cross-links: repeat for iseq_flowcell, eseq_flowcell, useq_wafer,
-- oseq_flowcell. The IN subqueries keep it on the id_*_tmp indexes.
SELECT st.id_lims, sa.id_lims, COUNT(*), COUNT(DISTINCT x.id_sample_tmp)
FROM pac_bio_run x
JOIN study st ON st.id_study_tmp = x.id_study_tmp
JOIN sample sa ON sa.id_sample_tmp = x.id_sample_tmp
WHERE x.id_study_tmp IN (SELECT id_study_tmp FROM study WHERE id_lims <> 'SQSCP')
   OR x.id_sample_tmp IN (SELECT id_sample_tmp FROM sample WHERE id_lims <> 'SQSCP')
GROUP BY 1, 2;

-- iRODS files for non-SQSCP samples (PacBio; for Illumina join
-- iseq_flowcell -> iseq_product_metrics on id_iseq_flowcell_tmp).
SELECT COUNT(DISTINCT il.id_seq_product_irods_locations_tmp)
FROM pac_bio_run r
JOIN pac_bio_product_metrics pm ON pm.id_pac_bio_tmp = r.id_pac_bio_tmp
JOIN seq_product_irods_locations il ON il.id_product = pm.id_pac_bio_product
WHERE r.id_sample_tmp IN (SELECT id_sample_tmp FROM sample WHERE id_lims <> 'SQSCP');
```
