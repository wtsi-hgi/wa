# MLWH domain glossary

This glossary defines the Multi-LIMS Warehouse (MLWH) domain entities and every
identifier kind exposed by the cache-backed `wa mlwh serve` REST API, for an
implementor with no prior MLWH or Sanger background. The machine-readable
schemas of every response live in the OpenAPI document at `GET /openapi.json`;
the catalogue of endpoints is in `api-reference.md`. This file is hand-authored
and explains _what the things mean_, not the wire format.

The **MLWH** (Multi-LIMS Warehouse) is a read-only data warehouse that mirrors
metadata from the Sanger Institute's Laboratory Information Management Systems
(LIMS), principally Sequencescape (whose LIMS code is `SQSCP`), together with
sequencing-run metrics and the iRODS data-object locations of the resulting
files. `wa mlwh serve` exposes a cache that mirrors a curated subset of MLWH;
every list and count endpoint is scoped to `id_lims = 'SQSCP'`, so "study" and
"sample" below mean the Sequencescape-tracked entities.

## Entities

These are the core nouns the API returns and relates.

### Study

A **study** is a unit of scientific work registered in the LIMS: a named
project that owns samples, defines the data-release and governance policy for
its data, and groups the sequencing done for it. In the warehouse a study is a
row in `study_mirror` carrying its LIMS study id, UUID, name and title, owning
programme and faculty sponsor, lifecycle state, reference genome, and the
governance fields that decide who may see its data (data-access group,
visibility, whether it contains human DNA, EGA data-access-committee and policy
accession numbers, data-release strategy and timing). A study is the usual
top-level entry point: from a study you can list its samples, libraries, runs,
and iRODS paths.

### Sample

A **sample** is a single biological specimen registered in the LIMS - the
physical material (e.g. DNA or RNA from one source) that is prepared into
libraries and sequenced. In the warehouse a sample is a row in `sample_mirror`
carrying its LIMS sample id and UUID, its Sanger sample id and name, the
supplier's own name for it, its public-archive accession, the donor it came
from, and its organism (NCBI taxon id and common name). A sample belongs to one
or more studies (via the libraries made from it) and is sequenced on one or more
lanes.

### Library

A **library** is the sequencing-ready preparation made from a sample (or pool of
samples): the molecular product, of a particular **library type**, that is
loaded onto the sequencer. A library is the link between samples and the studies
and runs they appear in: each library belongs to a study, is prepared from one
or more samples, and is sequenced on one or more lanes. In the warehouse a
library is identified by its pipeline LIMS id within a study, and may also carry
a library id and a LIMS library id. Listing the samples of a study walks through
its libraries (`library_samples`), which is why "samples for study" is a
distinct-sample count rather than a row count.

### Run

A **run** is one execution of a sequencing instrument, identified by its run id
(`id_run`). A run produces data for the samples whose libraries were loaded onto
it, divided across lanes. From a run you can list the samples sequenced on it and
the studies those samples belong to. Run-level and lane-level metrics are
mirrored from the `iseq_*` warehouse tables.

### Lane

A **lane** is one physical region of a sequencing flow cell within a run, and is
identified here by the triple (run id, lane position, tag index). Multiple
samples are typically multiplexed onto a single lane and distinguished by their
**tag index** (the index of the multiplexing barcode). A lane is therefore the
finest-grained sequencing location: a sample maps to the set of run/lane/tag
combinations on which it was sequenced.

### iRODS path

An **iRODS path** is the location of a sequencing data object in
[iRODS](https://irods.org/), the rule-oriented data-management system the
institute stores sequencing output in. Each iRODS path entry carries a product
identifier, the iRODS collection (directory) and data-object (file) names, and
the full iRODS path of the object. iRODS paths are exported per sample and per
study from the `seq_product_irods_locations` warehouse joins, and are how a
caller goes from "this sample/study" to "the actual data files".

## How the entities relate

```
study ──< library >── sample ──< lane (run, lane, tag) >── run
  │           │           │
  │           │           └──< iRODS path (data objects)
  │           │
  └───────────┴── iRODS path (per study and per sample)
```

- A **study** owns many **libraries**; each library belongs to one study.
- A **library** is prepared from one or more **samples**; a sample may appear in
  many libraries (and therefore in many studies).
- A **sample** is sequenced on one or more **lanes**; each lane is part of one
  **run**, so a sample maps to many (run, lane, tag) combinations and a run
  carries many samples.
- A **run** is associated with the **studies** of the samples sequenced on it.
- **iRODS paths** are the data objects produced for a sample or study; they are
  the bridge from metadata to the stored sequencing files.

## Availability, recency and progress

These are the domain concepts the availability, recency and sample-progress
endpoints (`/study/:id/overview`, `/study/:id/samples-with-data`,
`/study/:id/status-breakdown`, `/sample/:id/progress`, and their counterparts)
report on. They explain _what those answers mean_, not the wire format.

### Sequencing data available

A sample has **sequencing data available** for a study when it has at least one
row in the iRODS locations mirror (`seq_product_irods_locations_mirror`) scoped
by `id_study_lims = :id` - that is, real data files exist in iRODS for that
sample under that study - with the sample anchored to the study by its
`library_samples` membership. Availability is therefore **study-scoped**: it
means "data the sample has under this study" (scoped exactly as
`/study/:id/irods` is), NOT "data the sample has anywhere". A study's linked
samples partition into those with data available and those without
(`with_data + without_data = samples_total`); the without-data side includes
samples that were sequenced but produced no iRODS rows, registered-only samples,
and ONT samples.

### Added to iRODS

Recency - "data **added to iRODS** since X" - is measured by the iRODS `created`
timestamp, which records when the data object was created in iRODS. Every
recency window (e.g. the `since`/`until` query params and the overview's
`added_last_7_days`) filters on `created`, and **never** on `last_updated`
(the source `last_changed`, which conflates a newly-added row with one merely
modified later) or on `last_run` (which only records when `wa` last synced the
cache). Only `created` answers when data was genuinely added, so it is the field
every recency filter uses.

### Baseline phase

The **baseline phase** is the coarse, always-derivable progress phase reported
for every sample on every platform, so there is no sample for which progress
cannot be answered. It advances `registered` (linked to the study but with no
products) to `sequenced` (has product-metrics, with QC rolled up to
`pass`/`fail`/`pending`) to `delivered` (has study-scoped iRODS rows, with the
earliest `created` reported as the delivered date). A multi-platform sample
takes the most-advanced phase across its platforms. These three phases
(`registered`, `sequenced`, `delivered`) are a closed vocabulary.

### Detailed timeline

The **detailed timeline** is the ordered wet-lab and sequencing milestone
timeline for a sample, present only when the sample appears in the operations
tracking mirror (`seq_ops_tracking_per_sample_mirror`). When present it lists
the sample's reached milestones in canonical order and the progress response
sets `detailed_timeline=true`; when the sample is absent from the tracking
mirror the response sets `detailed_timeline=false` with a `timeline_reason`
(e.g. "not in tracking window") and still returns the baseline phase - the
absence of a detailed timeline is reported as less detail, never as an error.

### Platform

A **platform** is the sequencing platform a sample's data was produced on -
Illumina, PacBio, Elembio, Ultimagen, or ONT (Oxford Nanopore) - reported as the
canonical platform name derived from which product-metrics mirror the sample has
products in (Illumina, PacBio, Elembio, or Ultimagen), with ONT derived from
`oseq_flowcell` membership. It is determined by which platform's metrics the
sample appears in, never from the iRODS `seq_platform_name` source string.
Negatives are platform-qualified so a "no data" answer
always names the platform it applies to. ONT is a special case: only a sample's
identity and study are tracked for ONT, so its availability, QC, and
within-sequencing status are reported as `not_tracked` rather than as a false
zero.

## Identifier kinds

An **identifier kind** (`IdentifierKind`) names what a raw identifier _is_. The
`/classify`, `/resolve/*`, and `/expand/*` endpoints report the kind of an
identifier and its canonical value, so a natural-language layer can turn a bare
string a user typed into the right lookup. The complete set of kinds is below;
every value the API emits is one of these strings.

### Sample identifier kinds

- `sample_uuid` - the LIMS UUID of a sample (`uuid_sample_lims`), the globally
  unique Sequencescape identifier for the sample.
- `sample_lims_id` - the LIMS id of a sample (`id_sample_lims`), the sample's
  identifier within the owning LIMS.
- `sanger_sample_name` - the Sanger **sample name** (`sample_mirror.name`), the
  human-facing name the sample is known by at the institute. This is the
  identifier most other "samples for ..." lookups key on.
- `sanger_sample_id` - the **Sanger sample id** (`sanger_sample_id`), the
  institute-assigned sample identifier, distinct from the sample name.
- `supplier_name` - the name the **sample supplier** gave the sample
  (`supplier_name`); external-facing and not guaranteed unique.
- `sample_accession` - the public-archive **accession number** of the sample
  (`accession_number`), e.g. an ENA/EGA sample accession.
- `donor_id` - the **donor identifier** the sample was taken from (`donor_id`);
  one donor may have many samples.

### Study identifier kinds

- `study_uuid` - the LIMS UUID of a study (`uuid_study_lims`), the globally
  unique Sequencescape identifier for the study.
- `study_lims_id` - the LIMS id of a study (`id_study_lims`), the study's
  identifier within the owning LIMS; the canonical study key used throughout the
  API (e.g. `/study/:id/...`).
- `study_accession` - the public-archive **accession number** of the study
  (`accession_number`).
- `study_name` - the **study name** (`study_mirror.name`), the human-facing name
  of the study.

### Run identifier kind

- `run_id` - a sequencing **run identifier** (`id_run`), identifying one
  instrument run.

### Library identifier kinds

- `library_type` - the **library type**, the kind of library preparation (e.g. a
  named protocol). Used to resolve and list libraries by their type.
- `library_id` - a **library identifier** (`library_id`), identifying a specific
  library.
- `id_library_lims` - the **LIMS library identifier** (`id_library_lims`), the
  library's identifier within the owning LIMS. (Note the value of this kind is
  the literal string `id_library_lims`, matching the underlying column name.)

## Search semantics

The open-ended `/search/*` endpoints differ in how the term is matched:

- **Sample search** (`/search/sample/:term`) is a **word-prefix** match. The
  term matches the start of any whitespace/punctuation-delimited word in a
  sample's `name`, `supplier_name`, `common_name`, or `donor_id`
  (case-insensitive). So `musculus` and `mus` both match a sample whose
  `common_name` is "Mus Musculus", but a mid-word substring (e.g. `usculus`)
  does **not** match. This is backed by a word-token prefix index so it stays
  fast on the ~10M-row sample table; use the exact `Find*`/resolver lookups for
  precise identifier matches. The minimum term length is 3; shorter terms return
  nothing.
- **Study search** (`/search/study/:term`) is a plain **substring** match
  (case-insensitive `contains`) over a study's `name`, `study_title`,
  `programme`, or `faculty_sponsor`, on the small (~8k-row) study table. The
  minimum term length is 3.
