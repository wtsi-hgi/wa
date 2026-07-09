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

package mlwh

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	exportFormatTSV  = "tsv"
	exportFormatCSV  = "csv"
	exportFormatJSON = "json"

	defaultExportAllLimit = 1000
)

type exportRelationshipKind string

const (
	exportRelationshipIRODS       exportRelationshipKind = "irods"
	exportRelationshipSamples     exportRelationshipKind = "samples"
	exportRelationshipRuns        exportRelationshipKind = "runs"
	exportRelationshipLibraries   exportRelationshipKind = "libraries"
	exportRelationshipLanes       exportRelationshipKind = "lanes"
	exportRelationshipStudies     exportRelationshipKind = "studies"
	exportRelationshipUsers       exportRelationshipKind = "users"
	exportRelationshipSampleCRAMs exportRelationshipKind = "sample-crams"
	exportRelationshipProducts    exportRelationshipKind = "products"
)

type exportRelationshipSpec struct {
	Children    string
	Aliases     []string
	ParentKinds []string
	Description string
	Kind        exportRelationshipKind
}

var exportRelationshipSpecs = []exportRelationshipSpec{
	{
		Children:    "products",
		ParentKinds: []string{"study"},
		Description: "product-grained rows, one per distinct id_run/lane/tag, including products with no iRODS object",
		Kind:        exportRelationshipProducts,
	},
	{
		Children:    "sample-crams",
		ParentKinds: []string{"study"},
		Description: "one merged-aware CRAM row per sample",
		Kind:        exportRelationshipSampleCRAMs,
	},
	{
		Children:    "irods",
		Aliases:     []string{"files"},
		ParentKinds: []string{"study", "sample", "run"},
		Description: "iRODS data-object rows, usually file paths",
		Kind:        exportRelationshipIRODS,
	},
	{
		Children:    "samples",
		ParentKinds: []string{"study", "run", "library"},
		Description: "sample identity rows",
		Kind:        exportRelationshipSamples,
	},
	{
		Children:    "runs",
		ParentKinds: []string{"study", "sample"},
		Description: "sequencing run rows",
		Kind:        exportRelationshipRuns,
	},
	{
		Children:    "libraries",
		ParentKinds: []string{"study"},
		Description: "library rows",
		Kind:        exportRelationshipLibraries,
	},
	{
		Children:    "lanes",
		ParentKinds: []string{"sample"},
		Description: "run/lane/tag rows for a sample",
		Kind:        exportRelationshipLanes,
	},
	{
		Children:    "studies",
		ParentKinds: []string{"sample", "faculty-sponsor", "user", "programme"},
		Description: "study rows",
		Kind:        exportRelationshipStudies,
	},
	{
		Children:    "users",
		ParentKinds: []string{"study"},
		Description: "study_users rows for a study",
		Kind:        exportRelationshipUsers,
	},
}

type exportColumn struct {
	Name          string
	Aliases       []string
	NeedsSample   bool
	NeedsStudy    bool
	Supported     bool
	UnsupportedBy string
}

type exportVocabulary struct {
	Columns []exportColumn
	Default []string
}

var (
	irodsExportVocabulary = exportVocabulary{
		Columns: fileExportColumns(),
		Default: []string{"supplier_name", "sanger_sample_id", "manual_qc", "irods_path"},
	}
	sampleExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "id_sample_tmp", Supported: true},
			{Name: "id_lims", Supported: true},
			{Name: "id_sample_lims", Supported: true},
			{Name: "uuid_sample_lims", Supported: true},
			{Name: "name", Supported: true},
			{Name: "sanger_sample_id", Supported: true},
			{Name: "supplier_name", Aliases: []string{"supplier_sample_name"}, Supported: true},
			{Name: "accession_number", Supported: true},
			{Name: "donor_id", Supported: true},
			{Name: "taxon_id", Supported: true},
			{Name: "common_name", Supported: true},
			{Name: "description", Supported: true},
		},
		Default: []string{"name", "sanger_sample_id", "supplier_name", "accession_number"},
	}
	runExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "id", Supported: true},
			{Name: "native_id", Supported: true},
			{Name: "id_run", Supported: true},
			{Name: "platform", Supported: true},
			{Name: "manufacturer", Supported: true},
			{Name: "run_date", Supported: true},
			{Name: "date_basis", Supported: true},
		},
		Default: []string{"id_run"},
	}
	libraryExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "pipeline_id_lims", Supported: true},
			{Name: "id_study_lims", Supported: true},
			{Name: "library_id", Supported: true},
			{Name: "id_library_lims", Supported: true},
		},
		Default: []string{"pipeline_id_lims", "library_id", "id_library_lims"},
	}
	laneExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "id_run", Supported: true},
			{Name: "lane", Supported: true},
			{Name: "tag_index", Supported: true},
		},
		Default: []string{"id_run", "lane", "tag_index"},
	}
	studyExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "id_study_lims", Supported: true},
			{Name: "name", Supported: true},
			{Name: "accession_number", Supported: true},
			{Name: "study_title", Supported: true},
			{Name: "faculty_sponsor", Supported: true},
			{Name: "programme", Supported: true},
			{Name: "role", Supported: true},
		},
		Default: []string{"id_study_lims", "name", "accession_number"},
	}
	userExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "role", Supported: true},
			{Name: "name", Supported: true},
			{Name: "login", Supported: true},
			{Name: "email", Supported: true},
		},
		Default: []string{"role", "name", "login", "email"},
	}
	sampleCRAMExportVocabulary = exportVocabulary{
		Columns: fileExportColumns(),
		Default: []string{"name", "accession_number", "irods_path", "merged"},
	}
	productExportVocabulary = exportVocabulary{
		Columns: []exportColumn{
			{Name: "name", Supported: true},
			{Name: "supplier_name", Aliases: []string{"supplier_sample_name"}, Supported: true},
			{Name: "accession_number", Supported: true},
			{Name: "sanger_sample_id", Supported: true},
			{Name: "id_run", Supported: true},
			{Name: "lane", Aliases: []string{"position"}, Supported: true},
			{Name: "tag_index", Supported: true},
			{Name: "manual_qc", Supported: true},
			{Name: "irods_path", Supported: true},
			{Name: "irods_unmatched", Supported: true},
			{Name: "reason", Supported: true},
			{Name: "id_study_lims", Supported: true},
			{Name: "study_accession_number", Supported: true},
		},
		Default: []string{
			"name",
			"supplier_name",
			"accession_number",
			"sanger_sample_id",
			"id_run",
			"lane",
			"tag_index",
			"manual_qc",
		},
	}
)

func validateExportContinuationSupport(kind exportRelationshipKind, rel ExportRelationship, opts ExportOptions) error {
	if kind == exportRelationshipIRODS || kind == exportRelationshipProducts {
		return nil
	}
	if strings.TrimSpace(opts.Cursor) != "" {
		return fmt.Errorf(
			"%w: cursor pagination is supported only for iRODS and products exports; use limit/offset for %s of %s",
			ErrUnsupportedIdentifier,
			rel.Children,
			rel.ParentKind,
		)
	}

	return nil
}

func normaliseExportCreatedSort(kind exportRelationshipKind, raw string) (bool, error) {
	if kind != exportRelationshipIRODS || strings.TrimSpace(raw) == "" {
		return false, nil
	}

	return normaliseIRODSOrderBy(raw)
}

func normaliseExportCreatedWindow(kind exportRelationshipKind, since, until string) ([]any, error) {
	if kind != exportRelationshipIRODS {
		return nil, nil
	}

	return normaliseIRODSCreatedWindowArgs(since, until)
}

func exportFileFilters(kind exportRelationshipKind, opts ExportOptions) (string, bool, error) {
	if !exportRelationshipUsesFileType(kind) && !exportRelationshipAttachesFileType(kind) && strings.TrimSpace(opts.FileType) != "" {
		return "", false, fmt.Errorf("%w: --file-type applies only to file exports or product iRODS attachments", ErrUnsupportedIdentifier)
	}

	fileType := opts.FileType
	if exportRelationshipUsesFileType(kind) && strings.TrimSpace(fileType) == "" {
		fileType = "cram"
	}

	normalised, err := normaliseFileType(fileType)
	if err != nil {
		return "", false, err
	}

	deliverablesOnly := false
	if opts.DeliverablesOnly != nil {
		deliverablesOnly = *opts.DeliverablesOnly
	} else if exportRelationshipUsesFileType(kind) && normalised == "cram" {
		deliverablesOnly = true
	}

	return normalised, deliverablesOnly, nil
}

func exportRelationshipUsesFileType(kind exportRelationshipKind) bool {
	return kind == exportRelationshipIRODS || kind == exportRelationshipSampleCRAMs
}

func exportRelationshipAttachesFileType(kind exportRelationshipKind) bool {
	return kind == exportRelationshipProducts
}

func validateExportFilterSupport(kind exportRelationshipKind, rel ExportRelationship, filters exportFilters, deliverablesOnly bool) error {
	if !exportUsesSharedSampleFilters(filters, deliverablesOnly) {
		return nil
	}
	switch kind {
	case exportRelationshipIRODS, exportRelationshipSamples, exportRelationshipSampleCRAMs, exportRelationshipProducts:
		return nil
	}

	return fmt.Errorf(
		"%w: shared sample export filters are backed for iRODS/files, samples, sample-crams, and products exports; %s of %s is not filter-backed yet",
		ErrUnsupportedIdentifier,
		rel.Children,
		rel.ParentKind,
	)
}

func exportUsesSharedSampleFilters(filters exportFilters, deliverablesOnly bool) bool {
	return filters.QC != "" || filters.LibraryType != "" || filters.Organism != "" || deliverablesOnly
}

func exportRelationshipSpecMatchesChildren(spec exportRelationshipSpec, children string) bool {
	if children == spec.Children {
		return true
	}
	for _, alias := range spec.Aliases {
		if children == alias {
			return true
		}
	}

	return false
}

func fileExportColumns() []exportColumn {
	return []exportColumn{
		{Name: "supplier_name", Aliases: []string{"supplier_sample_name"}, NeedsSample: true, Supported: true},
		{Name: "sanger_sample_id", NeedsSample: true, Supported: true},
		{Name: "name", NeedsSample: true, Supported: true},
		{Name: "accession_number", NeedsSample: true, Supported: true},
		{Name: "study_accession_number", NeedsStudy: true, Supported: true},
		{Name: "id_study_lims", Supported: true},
		{Name: "manual_qc", Supported: true},
		{Name: "id_run", Supported: true},
		{Name: "lane", Aliases: []string{"position"}, Supported: true},
		{Name: "tag_index", Supported: true},
		{Name: "platform", Supported: true},
		{Name: "created", Supported: true},
		{Name: "merged", Supported: true},
		{Name: "deliverable", Supported: true},
		{Name: "irods_path", Supported: true},
		{Name: "id_product", Supported: true},
		{Name: "id_sample_tmp", Supported: true},
		{Name: "collection", Supported: true},
		{Name: "data_object", Supported: true},
	}
}

func resolveExportColumns(rel ExportRelationship, vocab exportVocabulary, requested []string) ([]exportColumn, error) {
	rawColumns := requested
	if len(rawColumns) == 0 {
		rawColumns = vocab.Default
	}

	lookup := exportColumnLookup(vocab.Columns)
	columns := make([]exportColumn, 0, len(rawColumns))
	for _, raw := range rawColumns {
		name := strings.ToLower(strings.TrimSpace(raw))
		column, ok := lookup[name]
		if !ok {
			return nil, unknownExportColumnError(rel, raw, vocab.Columns)
		}
		if !column.Supported {
			return nil, fmt.Errorf("%w: export column %q is not backed yet%s", ErrUnsupportedIdentifier, column.Name, column.UnsupportedBy)
		}

		columns = append(columns, column)
	}

	return columns, nil
}

func exportColumnLookup(columns []exportColumn) map[string]exportColumn {
	lookup := make(map[string]exportColumn, len(columns))
	for _, column := range columns {
		lookup[column.Name] = column
		for _, alias := range column.Aliases {
			lookup[alias] = column
		}
	}

	return lookup
}

func unknownExportColumnError(rel ExportRelationship, raw string, columns []exportColumn) error {
	return fmt.Errorf(
		"%w: unknown export column %q for %s of %s; valid columns: %s",
		ErrUnsupportedIdentifier,
		strings.TrimSpace(raw),
		rel.Children,
		rel.ParentKind,
		strings.Join(validExportColumnNames(columns), ", "),
	)
}

func validExportColumnNames(columns []exportColumn) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
		names = append(names, column.Aliases...)
	}

	return names
}

func exportColumnNames(columns []exportColumn) []string {
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = column.Name
	}

	return names
}

func projectExportIRODSRows(rows []exportIRODSRow, columns []exportColumn) [][]string {
	projected := make([][]string, len(rows))
	for rowIndex, row := range rows {
		projected[rowIndex] = make([]string, len(columns))
		for columnIndex, column := range columns {
			projected[rowIndex][columnIndex] = row.cell(column.Name)
		}
	}

	return projected
}

func projectExportProductRows(rows []exportProductRow, columns []exportColumn) [][]string {
	projected := make([][]string, len(rows))
	for rowIndex, row := range rows {
		projected[rowIndex] = make([]string, len(columns))
		for columnIndex, column := range columns {
			projected[rowIndex][columnIndex] = row.cell(column.Name)
		}
	}

	return projected
}

func projectSamples(samples []Sample, columns []exportColumn) [][]string {
	rows := make([][]string, len(samples))
	for rowIndex, sample := range samples {
		values := map[string]string{
			"id_sample_tmp":    strconv.FormatInt(sample.IDSampleTmp, 10),
			"id_lims":          sample.IDLims,
			"id_sample_lims":   sample.IDSampleLims,
			"uuid_sample_lims": sample.UUIDSampleLims,
			"name":             sample.Name,
			"sanger_sample_id": sample.SangerSampleID,
			"supplier_name":    sample.SupplierName,
			"accession_number": sample.AccessionNumber,
			"donor_id":         sample.DonorID,
			"taxon_id":         strconv.Itoa(sample.TaxonID),
			"common_name":      sample.CommonName,
			"description":      sample.Description,
		}
		rows[rowIndex] = projectMap(values, columns)
	}

	return rows
}

func projectRuns(runs []exportRunRow, columns []exportColumn) [][]string {
	rows := make([][]string, len(runs))
	for rowIndex, run := range runs {
		values := map[string]string{
			"id":           run.ID,
			"native_id":    run.NativeID,
			"id_run":       run.IDRun,
			"platform":     run.Platform,
			"manufacturer": run.Manufacturer,
			"run_date":     run.RunDate,
			"date_basis":   run.DateBasis,
		}
		rows[rowIndex] = projectMap(values, columns)
	}

	return rows
}

func projectLibraries(libraries []Library, columns []exportColumn) [][]string {
	rows := make([][]string, len(libraries))
	for rowIndex, library := range libraries {
		values := map[string]string{
			"pipeline_id_lims": library.PipelineIDLims,
			"id_study_lims":    library.IDStudyLims,
			"library_id":       library.LibraryID,
			"id_library_lims":  library.IDLibraryLims,
		}
		rows[rowIndex] = projectMap(values, columns)
	}

	return rows
}

func projectLanes(lanes []Lane, columns []exportColumn) [][]string {
	rows := make([][]string, len(lanes))
	for rowIndex, lane := range lanes {
		values := map[string]string{
			"id_run":    strconv.Itoa(lane.IDRun),
			"lane":      strconv.Itoa(lane.Position),
			"tag_index": strconv.Itoa(lane.TagIndex),
		}
		rows[rowIndex] = projectMap(values, columns)
	}

	return rows
}

func exportColumnsNeedSample(columns []exportColumn) bool {
	for _, column := range columns {
		if column.NeedsSample {
			return true
		}
	}

	return false
}

func exportColumnsNeedStudy(columns []exportColumn) bool {
	for _, column := range columns {
		if column.NeedsStudy {
			return true
		}
	}

	return false
}

func exportColumnsNeedIRODSAttachment(columns []exportColumn) bool {
	for _, column := range columns {
		switch column.Name {
		case "irods_path", "irods_unmatched", "reason":
			return true
		}
	}

	return false
}

func projectStudies(studies []Study, role string, columns []exportColumn) [][]string {
	rows := make([][]string, len(studies))
	for rowIndex, study := range studies {
		rows[rowIndex] = projectStudy(study, role, columns)
	}

	return rows
}

func projectPersonStudies(studies []PersonStudy, columns []exportColumn) [][]string {
	rows := make([][]string, len(studies))
	for rowIndex, study := range studies {
		rows[rowIndex] = projectStudy(study.Study, study.Role, columns)
	}

	return rows
}

func projectStudy(study Study, role string, columns []exportColumn) []string {
	values := map[string]string{
		"id_study_lims":    study.IDStudyLims,
		"name":             study.Name,
		"accession_number": study.AccessionNumber,
		"study_title":      study.StudyTitle,
		"faculty_sponsor":  study.FacultySponsor,
		"programme":        study.Programme,
		"role":             role,
	}

	return projectMap(values, columns)
}

func projectStudyUsers(users []StudyUser, columns []exportColumn) [][]string {
	rows := make([][]string, len(users))
	for rowIndex, user := range users {
		values := map[string]string{
			"role":  user.Role,
			"name":  user.Name,
			"login": user.Login,
			"email": user.Email,
		}
		rows[rowIndex] = projectMap(values, columns)
	}

	return rows
}

func projectSampleCRAMs(sampleCRAMs []exportSampleCRAMRow, columns []exportColumn) [][]string {
	rows := make([][]string, len(sampleCRAMs))
	for rowIndex, sampleCRAM := range sampleCRAMs {
		rows[rowIndex] = make([]string, len(columns))
		for columnIndex, column := range columns {
			rows[rowIndex][columnIndex] = sampleCRAM.cell(column.Name)
		}
	}

	return rows
}

func projectMap(values map[string]string, columns []exportColumn) []string {
	row := make([]string, len(columns))
	for index, column := range columns {
		row[index] = values[column.Name]
	}

	return row
}

func validateExportRunColumns(rows []exportRunRow, columns []exportColumn) error {
	needsRunDate := false
	for _, column := range columns {
		if column.Name == "run_date" || column.Name == "date_basis" {
			needsRunDate = true
			break
		}
	}
	if !needsRunDate {
		return nil
	}

	for _, row := range rows {
		if row.RunDate == "" {
			return fmt.Errorf(
				"%w: export run column \"run_date\" is unavailable for run %q because no authoritative %s date is available; select id/native_id/platform or sync run-status data",
				ErrUnsupportedIdentifier,
				row.ID,
				exportRunExpectedDateBasis(row),
			)
		}
	}

	return nil
}

func exportRunExpectedDateBasis(row exportRunRow) string {
	if row.DateBasis != "" {
		return row.DateBasis
	}
	for _, spec := range runAggregationPlatformSpecs {
		if spec.platform == row.Platform {
			return spec.dateBasis
		}
	}

	return "run"
}

func vocabularyForExportKind(kind exportRelationshipKind) exportVocabulary {
	switch kind {
	case exportRelationshipIRODS:
		return irodsExportVocabulary
	case exportRelationshipSamples:
		return sampleExportVocabulary
	case exportRelationshipRuns:
		return runExportVocabulary
	case exportRelationshipLibraries:
		return libraryExportVocabulary
	case exportRelationshipLanes:
		return laneExportVocabulary
	case exportRelationshipStudies:
		return studyExportVocabulary
	case exportRelationshipUsers:
		return userExportVocabulary
	case exportRelationshipSampleCRAMs:
		return sampleCRAMExportVocabulary
	case exportRelationshipProducts:
		return productExportVocabulary
	default:
		return exportVocabulary{}
	}
}

// ExportColumnVocabularies returns every selectable export column grouped by
// export child, using the same vocabularies as Export column validation.
func ExportColumnVocabularies() []ExportColumnVocabulary {
	vocabularies := make([]ExportColumnVocabulary, 0, len(exportRelationshipSpecs))
	for _, spec := range exportRelationshipSpecs {
		vocab := vocabularyForExportKind(spec.Kind)
		columns := make([]ExportColumnDescription, len(vocab.Columns))
		for index, column := range vocab.Columns {
			columns[index] = ExportColumnDescription{
				Name:    column.Name,
				Aliases: append([]string(nil), column.Aliases...),
			}
		}

		vocabularies = append(vocabularies, ExportColumnVocabulary{
			Children: spec.Children,
			Aliases:  append([]string(nil), spec.Aliases...),
			Default:  append([]string(nil), vocab.Default...),
			Columns:  columns,
		})
	}

	return vocabularies
}

// ExportRelationship identifies the children to project and their parent kind,
// for example ExportRelationship{Children: "irods", ParentKind: "study"}.
type ExportRelationship struct{ Children, ParentKind string }

func normaliseExportRelationship(rel ExportRelationship) (ExportRelationship, exportRelationshipKind, error) {
	children := strings.ToLower(strings.TrimSpace(rel.Children))
	parentKind := strings.ToLower(strings.TrimSpace(rel.ParentKind))

	for _, spec := range exportRelationshipSpecs {
		if !exportRelationshipSpecMatchesChildren(spec, children) {
			continue
		}
		for _, allowedParentKind := range spec.ParentKinds {
			if parentKind == allowedParentKind {
				return ExportRelationship{Children: spec.Children, ParentKind: parentKind}, spec.Kind, nil
			}
		}
	}

	return ExportRelationship{}, "", fmt.Errorf("%w: unsupported export relationship %q of %q", ErrUnsupportedIdentifier, rel.Children, rel.ParentKind)
}

// Export projects a parent-child relationship into ordered string rows for the
// chosen columns.
func (c *Client) Export(ctx context.Context, rel ExportRelationship, parentID string, opts ExportOptions) (ExportResult, error) {
	plan, err := newExportPlan(rel, parentID, opts)
	if err != nil {
		return ExportResult{}, err
	}

	parent, err := c.resolveExportParent(ctx, plan, parentID)
	if err != nil {
		return ExportResult{}, err
	}

	if plan.all {
		return c.exportAll(ctx, plan, parent)
	}

	return c.exportPage(ctx, plan, parent)
}

func newExportPlan(rel ExportRelationship, parentID string, opts ExportOptions) (exportPlan, error) {
	normalisedRel, kind, err := normaliseExportRelationship(rel)
	if err != nil {
		return exportPlan{}, err
	}
	if strings.TrimSpace(parentID) == "" {
		return exportPlan{}, fmt.Errorf("%w: export parent id is required", ErrUnsupportedIdentifier)
	}
	if kind != exportRelationshipIRODS && (opts.Sort != "" || opts.Since != "" || opts.Until != "") {
		return exportPlan{}, fmt.Errorf("%w: created-date export sorting/windows are supported only for iRODS exports", ErrUnsupportedIdentifier)
	}
	if err = validateExportContinuationSupport(kind, normalisedRel, opts); err != nil {
		return exportPlan{}, err
	}
	sortCreatedDesc, err := normaliseExportCreatedSort(kind, opts.Sort)
	if err != nil {
		return exportPlan{}, err
	}
	if sortCreatedDesc && strings.TrimSpace(opts.Cursor) != "" {
		return exportPlan{}, fmt.Errorf("%w: created-date sorted iRODS exports require bounded limit/offset pages", ErrUnsupportedIdentifier)
	}
	createdWindowArgs, err := normaliseExportCreatedWindow(kind, opts.Since, opts.Until)
	if err != nil {
		return exportPlan{}, err
	}

	vocab := vocabularyForExportKind(kind)
	columns, err := resolveExportColumns(normalisedRel, vocab, opts.Columns)
	if err != nil {
		return exportPlan{}, err
	}

	format, err := normaliseExportFormat(opts.Format)
	if err != nil {
		return exportPlan{}, err
	}
	filters, err := exportFiltersFromOptions(opts)
	if err != nil {
		return exportPlan{}, err
	}
	normalisedFile, deliverablesOnly, err := exportFileFilters(kind, opts)
	if err != nil {
		return exportPlan{}, err
	}
	if err = validateExportFilterSupport(kind, normalisedRel, filters, deliverablesOnly); err != nil {
		return exportPlan{}, err
	}
	limit, offset, err := exportPaging(opts)
	if err != nil {
		return exportPlan{}, err
	}
	cursor := exportCursor{}
	if kind == exportRelationshipIRODS || kind == exportRelationshipProducts {
		cursor, err = decodeExportCursor(opts.Cursor)
		if err != nil {
			return exportPlan{}, err
		}
	}

	return exportPlan{
		rel:               normalisedRel,
		kind:              kind,
		columns:           columns,
		format:            format,
		needsIRODS:        kind == exportRelationshipProducts && exportColumnsNeedIRODSAttachment(columns),
		normalisedFile:    normalisedFile,
		deliverablesOnly:  deliverablesOnly,
		role:              opts.Role,
		filters:           filters,
		limit:             limit,
		offset:            offset,
		all:               opts.All,
		cursor:            cursor,
		sortCreatedDesc:   sortCreatedDesc,
		createdWindowArgs: createdWindowArgs,
	}, nil
}

func normaliseExportFormat(format string) (string, error) {
	switch normalised := strings.ToLower(strings.TrimSpace(format)); normalised {
	case "":
		return exportFormatTSV, nil
	case exportFormatTSV, exportFormatCSV, exportFormatJSON:
		return normalised, nil
	default:
		return "", fmt.Errorf("%w: unsupported export format %q (valid: tsv, csv, json)", ErrUnsupportedIdentifier, format)
	}
}

func exportFiltersFromOptions(opts ExportOptions) (exportFilters, error) {
	qc := strings.ToLower(strings.TrimSpace(opts.QC))
	switch qc {
	case "", qcPass, qcFail, qcPending:
	default:
		return exportFilters{}, fmt.Errorf("%w: unsupported export qc %q (valid: pass, fail, pending)", ErrUnsupportedIdentifier, opts.QC)
	}

	return exportFilters{
		QC:          qc,
		LibraryType: strings.TrimSpace(opts.LibraryType),
		Organism:    strings.TrimSpace(opts.Organism),
	}, nil
}

func exportPaging(opts ExportOptions) (int, int, error) {
	if opts.Limit < 0 || opts.Offset < 0 {
		return 0, 0, fmt.Errorf("%w: export limit and offset must be non-negative", ErrUnsupportedIdentifier)
	}
	if opts.Limit == 0 && opts.Offset > 0 && !opts.All {
		return 0, 0, fmt.Errorf("%w: export offset requires an explicit limit", ErrUnsupportedIdentifier)
	}

	limit := opts.Limit
	if limit == 0 {
		limit = defaultExportAllLimit
	}

	return limit, opts.Offset, nil
}

func decodeExportCursor(raw string) (exportCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return exportCursor{}, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return exportCursor{}, fmt.Errorf("%w: invalid export cursor", ErrUnsupportedIdentifier)
	}

	parts := strings.Split(string(decoded), "\t")
	if len(parts) != 4 {
		return exportCursor{}, fmt.Errorf("%w: invalid export cursor", ErrUnsupportedIdentifier)
	}

	values := make([]int64, 4)
	for index, part := range parts {
		values[index], err = strconv.ParseInt(part, 10, 64)
		if err != nil {
			return exportCursor{}, fmt.Errorf("%w: invalid export cursor", ErrUnsupportedIdentifier)
		}
	}

	return exportCursor{
		IDRun:                values[0],
		Position:             values[1],
		TagIndex:             values[2],
		IDSeqProductLocation: values[3],
		set:                  true,
	}, nil
}

// ExportRelationshipDescription describes one supported export child vocabulary
// entry for CLI/help consumers.
type ExportRelationshipDescription struct {
	Children    string
	Aliases     []string
	ParentKinds []string
	Description string
}

// ExportRelationshipDescriptions returns the supported export child/parent
// vocabulary in the same order used by export relationship validation.
func ExportRelationshipDescriptions() []ExportRelationshipDescription {
	descriptions := make([]ExportRelationshipDescription, len(exportRelationshipSpecs))
	for index, spec := range exportRelationshipSpecs {
		descriptions[index] = ExportRelationshipDescription{
			Children:    spec.Children,
			Aliases:     append([]string(nil), spec.Aliases...),
			ParentKinds: append([]string(nil), spec.ParentKinds...),
			Description: spec.Description,
		}
	}

	return descriptions
}

// ExportColumnDescription describes one selectable export column.
type ExportColumnDescription struct {
	Name    string
	Aliases []string
}

// ExportColumnVocabulary describes the selectable columns for one export child.
type ExportColumnVocabulary struct {
	Children string
	Aliases  []string
	Default  []string
	Columns  []ExportColumnDescription
}

// ExportOptions controls column selection, shared filters, paging, and rendering
// format for Export. Limit/Offset/Cursor request a bounded page; All streams the
// complete result using internal pages. A nil DeliverablesOnly applies the
// relationship default: CRAM file listings exclude known controls/sub-products.
type ExportOptions struct {
	Columns          []string
	FileType         string
	DeliverablesOnly *bool
	Role             string
	QC               string
	LibraryType      string
	Organism         string
	Sort             string
	Since, Until     string
	Limit, Offset    int
	All              bool
	Cursor           string
	Format           string
}

// ExportResult is the string-rendered projection returned by Export. Rows are in
// the same order as Columns. Total is -1 for complete streaming exports.
type ExportResult struct {
	Columns    []string
	Rows       [][]string
	Total      int
	NextCursor string
	Complete   bool
	Format     string
	streamRows func(context.Context, func([]string) error) (int, error)
}

func emptyExportResult(plan exportPlan, total int) ExportResult {
	return ExportResult{
		Columns:  exportColumnNames(plan.columns),
		Rows:     [][]string{},
		Total:    total,
		Complete: true,
		Format:   plan.format,
	}
}

func buildExportResult(plan exportPlan, rows [][]string, total int) ExportResult {
	complete := total <= plan.offset+len(rows)
	return ExportResult{
		Columns:  exportColumnNames(plan.columns),
		Rows:     rows,
		Total:    total,
		Complete: complete,
		Format:   plan.format,
	}
}

func streamingExportResult(plan exportPlan, streamRows func(context.Context, func([]string) error) (int, error)) ExportResult {
	return ExportResult{
		Columns:    exportColumnNames(plan.columns),
		Rows:       nil,
		Total:      -1,
		Complete:   true,
		Format:     plan.format,
		streamRows: streamRows,
	}
}

// Render serialises the export result in its selected format.
func (r ExportResult) Render() (string, error) {
	return r.RenderAs(r.Format)
}

// RenderAs serialises the export result as tsv, csv, or json.
func (r ExportResult) RenderAs(format string) (string, error) {
	var buffer bytes.Buffer
	if _, err := r.RenderAsTo(context.Background(), &buffer, format); err != nil {
		return "", err
	}

	return buffer.String(), nil
}

// ForEachRow emits each export row without requiring the full result set to be
// held in memory.
func (r ExportResult) ForEachRow(ctx context.Context, emit func([]string) error) (int, error) {
	if r.streamRows != nil {
		return r.streamRows(ctx, emit)
	}

	count := 0
	for _, row := range r.Rows {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		if err := emit(row); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

// RenderTo serialises the export result to writer in its selected format,
// streaming rows when the result was produced with ExportOptions.All.
func (r ExportResult) RenderTo(ctx context.Context, writer io.Writer) (int, error) {
	return r.RenderAsTo(ctx, writer, r.Format)
}

// RenderAsTo serialises the export result as tsv, csv, or json to writer,
// returning the number of data rows emitted.
func (r ExportResult) RenderAsTo(ctx context.Context, writer io.Writer, format string) (int, error) {
	normalised, err := normaliseExportFormat(format)
	if err != nil {
		return 0, err
	}

	switch normalised {
	case exportFormatJSON:
		return r.renderJSONTo(ctx, writer)
	case exportFormatCSV:
		return r.renderDelimitedTo(ctx, writer, ',')
	default:
		return r.renderDelimitedTo(ctx, writer, '\t')
	}
}

func (r ExportResult) renderDelimitedTo(ctx context.Context, writer io.Writer, comma rune) (int, error) {
	csvWriter := csv.NewWriter(writer)
	csvWriter.Comma = comma
	wroteHeader := false

	count, err := r.ForEachRow(ctx, func(row []string) error {
		if !wroteHeader {
			if writeErr := csvWriter.Write(r.Columns); writeErr != nil {
				return writeErr
			}
			wroteHeader = true
		}

		return csvWriter.Write(row)
	})
	if err != nil && !wroteHeader {
		return count, err
	}
	if !wroteHeader {
		if writeErr := csvWriter.Write(r.Columns); writeErr != nil {
			return count, writeErr
		}
	}
	csvWriter.Flush()
	if writerErr := csvWriter.Error(); writerErr != nil {
		return count, writerErr
	}

	return count, err
}

func (r ExportResult) renderJSONTo(ctx context.Context, writer io.Writer) (int, error) {
	wroteStart := false
	count, err := r.ForEachRow(ctx, func(row []string) error {
		if !wroteStart {
			if _, writeErr := io.WriteString(writer, "["); writeErr != nil {
				return writeErr
			}
			wroteStart = true
		} else {
			if _, writeErr := io.WriteString(writer, ","); writeErr != nil {
				return writeErr
			}
		}

		object := make(map[string]string, len(r.Columns))
		for columnIndex, column := range r.Columns {
			object[column] = row[columnIndex]
		}

		encoded, marshalErr := json.Marshal(object)
		if marshalErr != nil {
			return marshalErr
		}

		_, writeErr := writer.Write(encoded)

		return writeErr
	})
	if err != nil {
		return count, err
	}
	if !wroteStart {
		if _, err = io.WriteString(writer, "[]\n"); err != nil {
			return count, err
		}

		return count, nil
	}
	if _, err = io.WriteString(writer, "]\n"); err != nil {
		return count, err
	}

	return count, nil
}

func (c *Client) exportAll(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	switch plan.kind {
	case exportRelationshipIRODS:
		return c.exportIRODS(ctx, plan, parent)
	case exportRelationshipProducts:
		return c.exportProducts(ctx, plan, parent)
	}

	return ExportResult{
		Columns:  exportColumnNames(plan.columns),
		Rows:     nil,
		Total:    -1,
		Complete: true,
		Format:   plan.format,
		streamRows: func(streamCtx context.Context, emit func([]string) error) (int, error) {
			return c.streamExportPages(streamCtx, plan, parent, emit)
		},
	}, nil
}

func (c *Client) exportPage(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	switch plan.kind {
	case exportRelationshipIRODS:
		return c.exportIRODS(ctx, plan, parent)
	case exportRelationshipProducts:
		return c.exportProducts(ctx, plan, parent)
	case exportRelationshipSamples:
		return c.exportSamples(ctx, plan, parent)
	case exportRelationshipRuns:
		return c.exportRuns(ctx, plan, parent)
	case exportRelationshipLibraries:
		return c.exportLibraries(ctx, plan, parent)
	case exportRelationshipLanes:
		return c.exportLanes(ctx, plan, parent)
	case exportRelationshipStudies:
		return c.exportStudies(ctx, plan, parent)
	case exportRelationshipUsers:
		return c.exportUsers(ctx, plan, parent)
	case exportRelationshipSampleCRAMs:
		return c.exportSampleCRAMs(ctx, plan, parent)
	default:
		return ExportResult{}, fmt.Errorf("%w: export relationship %s of %s is not backed yet", ErrUnsupportedIdentifier, plan.rel.Children, plan.rel.ParentKind)
	}
}

func (c *Client) exportIRODS(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	db := c.readCacheDB()
	if db == nil {
		return ExportResult{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	commonNames, err := c.exportOrganismCommonNames(ctx, db, plan.filters.Organism)
	if err != nil {
		return ExportResult{}, err
	}
	if plan.filters.Organism != "" && len(commonNames) == 0 {
		return emptyExportResult(plan, 0), nil
	}

	input := exportIRODSQueryInput{
		parentKind:          plan.rel.ParentKind,
		parentValue:         parent.Value,
		columns:             plan.columns,
		normalisedFile:      plan.normalisedFile,
		deliverablesOnly:    plan.deliverablesOnly,
		filters:             plan.filters,
		organismCommonNames: commonNames,
		limit:               plan.limit,
		offset:              plan.offset,
		cursor:              plan.cursor,
		sortCreatedDesc:     plan.sortCreatedDesc,
		createdWindowArgs:   plan.createdWindowArgs,
	}

	if plan.all {
		return c.exportIRODSAll(ctx, db, plan, input)
	}

	total, err := c.exportIRODSTotal(ctx, db, input)
	if err != nil {
		return ExportResult{}, err
	}

	input.limit = plan.limit + 1
	rows, err := c.queryExportIRODSRows(ctx, db, input)
	if err != nil {
		return ExportResult{}, err
	}

	more := len(rows) > plan.limit
	if more {
		rows = rows[:plan.limit]
	}

	result := ExportResult{
		Columns:  exportColumnNames(plan.columns),
		Rows:     projectExportIRODSRows(rows, plan.columns),
		Total:    total,
		Complete: !more,
		Format:   plan.format,
	}
	if more && !plan.sortCreatedDesc {
		result.NextCursor = encodeExportCursor(rows[len(rows)-1].cursor())
	}

	return result, nil
}

func encodeExportCursor(cursor exportCursor) string {
	raw := strings.Join([]string{
		strconv.FormatInt(cursor.IDRun, 10),
		strconv.FormatInt(cursor.Position, 10),
		strconv.FormatInt(cursor.TagIndex, 10),
		strconv.FormatInt(cursor.IDSeqProductLocation, 10),
	}, "\t")

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func (c *Client) exportIRODSAll(ctx context.Context, db *sql.DB, plan exportPlan, input exportIRODSQueryInput) (ExportResult, error) {
	return ExportResult{
		Columns:  exportColumnNames(plan.columns),
		Rows:     nil,
		Total:    -1,
		Complete: true,
		Format:   plan.format,
		streamRows: func(streamCtx context.Context, emit func([]string) error) (int, error) {
			return c.streamExportIRODSRows(streamCtx, db, plan, input, emit)
		},
	}, nil
}

func (c *Client) exportProducts(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	db := c.readCacheDB()
	if db == nil {
		return ExportResult{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	commonNames, err := c.exportOrganismCommonNames(ctx, db, plan.filters.Organism)
	if err != nil {
		return ExportResult{}, err
	}
	if plan.filters.Organism != "" && len(commonNames) == 0 {
		if plan.all {
			return streamingExportResult(plan, func(context.Context, func([]string) error) (int, error) {
				return 0, nil
			}), nil
		}

		return emptyExportResult(plan, 0), nil
	}

	input := exportProductQueryInput{
		studyID:             parent.Canonical,
		needsIRODS:          plan.needsIRODS,
		normalisedFile:      plan.normalisedFile,
		deliverablesOnly:    plan.deliverablesOnly,
		filters:             plan.filters,
		organismCommonNames: commonNames,
		limit:               plan.limit,
		offset:              plan.offset,
		cursor:              plan.cursor,
	}
	if plan.all {
		return c.exportProductsAll(ctx, db, plan, input, parent)
	}

	total, err := c.exportProductTotal(ctx, input)
	if err != nil {
		return ExportResult{}, err
	}

	input.limit = plan.limit + 1
	rows, err := c.queryExportProductRows(ctx, db, input, plan, parent)
	if err != nil {
		return ExportResult{}, err
	}
	if total == 0 && len(rows) == 0 {
		if err = c.requireStudyProductExportEmpty(ctx, parent.Canonical, plan.needsIRODS); err != nil {
			return ExportResult{}, err
		}

		return emptyExportResult(plan, 0), nil
	}
	if plan.needsIRODS {
		if err = c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); err != nil {
			return ExportResult{}, err
		}
	}

	more := len(rows) > plan.limit
	if more {
		rows = rows[:plan.limit]
	}

	result := ExportResult{
		Columns:  exportColumnNames(plan.columns),
		Rows:     projectExportProductRows(rows, plan.columns),
		Total:    total,
		Complete: !more,
		Format:   plan.format,
	}
	if more {
		result.NextCursor = encodeExportCursor(rows[len(rows)-1].cursor())
	}

	return result, nil
}

func (c *Client) exportProductsAll(ctx context.Context, db *sql.DB, plan exportPlan, input exportProductQueryInput, parent exportParent) (ExportResult, error) {
	if plan.needsIRODS {
		if err := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); err != nil {
			return ExportResult{}, err
		}
	}

	return streamingExportResult(plan, func(streamCtx context.Context, emit func([]string) error) (int, error) {
		return c.streamExportProductsRows(streamCtx, db, plan, input, parent, emit)
	}), nil
}

func (c *Client) exportSamples(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	samples, total, err := c.samplesForExport(ctx, plan, parent)
	if err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, projectSamples(samples, plan.columns), total), nil
}

func (c *Client) exportRuns(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	runs, total, err := c.runRowsForExport(ctx, plan, parent)
	if err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, projectRuns(runs, plan.columns), total), nil
}

func (c *Client) exportLibraries(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	libraries, err := c.LibrariesForStudy(ctx, parent.Canonical, plan.limit, plan.offset)
	total, countErr := c.CountLibrariesForStudy(ctx, parent.Canonical)
	if err = errors.Join(err, countErr); err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, projectLibraries(libraries, plan.columns), exportCountValue(total)), nil
}

func exportCountValue(count Count) int {
	return count.Count
}

func (c *Client) exportLanes(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	lanes, err := c.LanesForSample(ctx, parent.Canonical, plan.limit, plan.offset)
	total, countErr := c.CountLanesForSample(ctx, parent.Canonical)
	if err = errors.Join(err, countErr); err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, projectLanes(lanes, plan.columns), exportCountValue(total)), nil
}

func (c *Client) exportStudies(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	studies, err := c.studiesForExport(ctx, plan, parent)
	if err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, studies.rows, studies.total), nil
}

func (c *Client) exportUsers(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	users, err := c.StudyUsers(ctx, parent.Canonical, plan.role, plan.limit, plan.offset)
	total, countErr := c.CountStudyUsers(ctx, parent.Canonical, plan.role)
	if err = errors.Join(err, countErr); err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, projectStudyUsers(users, plan.columns), exportCountValue(total)), nil
}

func (c *Client) exportSampleCRAMs(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	db := c.readCacheDB()
	if db == nil {
		return ExportResult{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	commonNames, err := c.exportOrganismCommonNames(ctx, db, plan.filters.Organism)
	if err != nil {
		return ExportResult{}, err
	}
	if plan.filters.Organism != "" && len(commonNames) == 0 {
		return emptyExportResult(plan, 0), nil
	}

	input := exportSampleCRAMQueryInput{
		studyID:             parent.Canonical,
		normalisedFile:      plan.normalisedFile,
		deliverablesOnly:    plan.deliverablesOnly,
		filters:             plan.filters,
		organismCommonNames: commonNames,
		limit:               plan.limit,
		offset:              plan.offset,
	}
	total, err := c.exportSampleCRAMTotal(ctx, db, input)
	if err != nil {
		return ExportResult{}, err
	}
	rows, err := c.queryExportSampleCRAMRows(ctx, db, input)
	if err != nil {
		return ExportResult{}, err
	}

	return buildExportResult(plan, projectSampleCRAMs(rows, plan.columns), total), nil
}

type exportFilters struct {
	QC          string
	LibraryType string
	Organism    string
}

type exportSampleCRAMQueryInput struct {
	studyID             string
	normalisedFile      string
	deliverablesOnly    bool
	filters             exportFilters
	organismCommonNames []string
	limit               int
	offset              int
}

func (c *Client) exportSampleCRAMTotal(ctx context.Context, db *sql.DB, input exportSampleCRAMQueryInput) (int, error) {
	query, args := exportSampleCRAMCountQuery(input)

	return c.queryCount(ctx, query, "count export sample crams", args...)
}

func exportSampleCRAMCountQuery(input exportSampleCRAMQueryInput) (string, []any) {
	ranked, args := exportSampleCRAMRankedQuery(input)
	query := `SELECT COUNT(*) FROM (` + ranked + `) ranked ` +
		`INNER JOIN sample_mirror sm ON sm.id_sample_tmp = ranked.id_sample_tmp AND sm.id_lims = 'SQSCP' ` +
		`WHERE ranked.rn = 1`
	query, args = appendExportSampleCRAMSampleFilters(query, args, input)

	return query, args
}

func (c *Client) queryExportSampleCRAMRows(ctx context.Context, db *sql.DB, input exportSampleCRAMQueryInput) ([]exportSampleCRAMRow, error) {
	query, args := exportSampleCRAMPageQuery(input)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query export sample crams: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	sampleCRAMs := make([]exportSampleCRAMRow, 0)
	for rows.Next() {
		row, scanErr := scanExportIRODSRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan export sample cram row: %w", ErrUpstreamImpaired, scanErr)
		}
		sampleCRAMs = append(sampleCRAMs, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query export sample crams: %w", ErrUpstreamImpaired, err)
	}

	if len(sampleCRAMs) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []exportSampleCRAMRow{}, syncErr
			}

			return nil, syncErr
		}
	}

	return sampleCRAMs, nil
}

func exportSampleCRAMPageQuery(input exportSampleCRAMQueryInput) (string, []any) {
	ranked, args := exportSampleCRAMRankedQuery(input)
	query := `SELECT ranked.id_seq_product_irods_locations_tmp, ranked.id_iseq_product, ranked.irods_collection, ranked.irods_file_name, ` +
		`ranked.id_sample_tmp, ranked.id_study_lims, COALESCE(ranked.created, ''), ranked.platform, ranked.id_run, ranked.position, ` +
		`ranked.tag_index, ranked.qc, ranked.is_deliverable, ranked.merged, COALESCE(sm.name, ''), COALESCE(sm.supplier_name, ''), ` +
		`COALESCE(sm.sanger_sample_id, ''), COALESCE(sm.accession_number, ''), COALESCE(study_mirror.accession_number, '') FROM (` + ranked +
		`) ranked INNER JOIN sample_mirror sm ON sm.id_sample_tmp = ranked.id_sample_tmp AND sm.id_lims = 'SQSCP' ` +
		`LEFT JOIN study_mirror ON study_mirror.id_study_lims = ranked.id_study_lims AND study_mirror.id_lims = 'SQSCP' WHERE ranked.rn = 1`
	query, args = appendExportSampleCRAMSampleFilters(query, args, input)
	query += ` ORDER BY sm.name, ranked.id_sample_tmp LIMIT ? OFFSET ?`
	args = append(args, input.limit, input.offset)

	return query, args
}

func exportSampleCRAMRankedQuery(input exportSampleCRAMQueryInput) (string, []any) {
	where, args := exportSampleCRAMWhere(input)
	query := `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_iseq_product, spi.id_sample_tmp, spi.id_study_lims, ` +
		`spi.irods_collection, spi.irods_file_name, spi.created, spi.platform, spi.id_run, spi.position, spi.tag_index, ` +
		`spi.qc, spi.is_deliverable, spi.merged, ` +
		`ROW_NUMBER() OVER (PARTITION BY spi.id_sample_tmp ORDER BY ` +
		`CASE WHEN spi.merged <> 0 THEN 0 ELSE 1 END, ` +
		`spi.id_run, spi.position, spi.tag_index, spi.id_seq_product_irods_locations_tmp) AS rn ` +
		`FROM seq_product_irods_locations_mirror spi` + where

	return query, args
}

func exportSampleCRAMWhere(input exportSampleCRAMQueryInput) (string, []any) {
	query := ` WHERE spi.id_study_lims = ?`
	args := []any{input.studyID}
	if input.normalisedFile != "" {
		query += ` AND LOWER(spi.irods_file_name) LIKE ?`
		args = append(args, irodsFileTypeLikePattern(input.normalisedFile))
	}
	if input.deliverablesOnly {
		query += ` AND (spi.is_deliverable = 1 OR spi.is_deliverable IS NULL)`
	}
	query, args = appendExportIRODSQCWhere(query, args, input.filters.QC)
	if input.filters.LibraryType != "" {
		query += ` AND EXISTS (SELECT 1 FROM library_samples ls WHERE ls.id_sample_tmp = spi.id_sample_tmp AND ls.pipeline_id_lims = ? AND ls.id_study_lims = spi.id_study_lims)`
		args = append(args, input.filters.LibraryType)
	}
	return query, args
}

func appendExportIRODSQCWhere(query string, args []any, qc string) (string, []any) {
	switch qc {
	case qcPass:
		return query + ` AND spi.qc = 1`, args
	case qcFail:
		return query + ` AND spi.qc = 0`, args
	case qcPending:
		return query + ` AND spi.qc IS NULL AND LOWER(spi.platform) <> 'ont'`, args
	default:
		return query, args
	}
}

func appendExportSampleCRAMSampleFilters(query string, args []any, input exportSampleCRAMQueryInput) (string, []any) {
	if len(input.organismCommonNames) == 0 {
		return query, args
	}

	query += ` AND sm.common_name IN (` + placeholders(len(input.organismCommonNames)) + `)`
	for _, commonName := range input.organismCommonNames {
		args = append(args, commonName)
	}

	return query, args
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}

func (row exportIRODSRow) IRODSPath() string {
	return strings.TrimRight(row.Collection, "/") + "/" + row.FileName
}

type exportSampleCRAMRow = exportIRODSRow

type exportParent struct {
	Value     any
	Canonical string
	Study     *Study
	Sample    *Sample
	Run       *Run
	Library   *Library
}

func (c *Client) resolveExportParent(ctx context.Context, plan exportPlan, parentID string) (exportParent, error) {
	switch plan.rel.ParentKind {
	case "study":
		match, err := c.resolveExportStudy(ctx, parentID)
		if err != nil {
			return exportParent{}, err
		}

		return exportParent{Value: match.Study.IDStudyLims, Canonical: match.Study.IDStudyLims, Study: match.Study}, nil
	case "sample":
		match, err := c.ResolveSample(ctx, parentID)
		if err != nil {
			return exportParent{}, err
		}

		return exportParent{Value: match.Sample.IDSampleTmp, Canonical: match.Sample.Name, Sample: match.Sample}, nil
	case "run":
		match, err := c.ResolveRun(ctx, parentID)
		if err != nil {
			return exportParent{}, err
		}

		return exportParent{Value: match.Run.IDRun, Canonical: strconv.Itoa(match.Run.IDRun), Run: match.Run}, nil
	case "library":
		match, err := c.ResolveLibrary(ctx, parentID)
		if err != nil {
			return exportParent{}, err
		}

		return exportParent{Value: match.Canonical, Canonical: match.Canonical, Library: match.Library}, nil
	default:
		return exportParent{Value: parentID, Canonical: parentID}, nil
	}
}

func (c *Client) streamExportProductsRows(
	ctx context.Context,
	db *sql.DB,
	plan exportPlan,
	input exportProductQueryInput,
	parent exportParent,
	emit func([]string) error,
) (int, error) {
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}

		input.limit = plan.limit
		page, err := c.queryExportProductRows(ctx, db, input, plan, parent)
		if err != nil {
			return count, err
		}
		if len(page) == 0 {
			if count == 0 {
				if err = c.requireStudyProductExportEmpty(ctx, parent.Canonical, plan.needsIRODS); err != nil {
					return count, err
				}
			}

			return count, nil
		}

		for _, row := range projectExportProductRows(page, plan.columns) {
			if err := emit(row); err != nil {
				return count, err
			}
			count++
		}
		if len(page) < plan.limit {
			return count, nil
		}

		input.cursor = page[len(page)-1].cursor()
		input.offset = 0
	}
}

func (c *Client) queryExportProductRows(
	ctx context.Context,
	db *sql.DB,
	input exportProductQueryInput,
	plan exportPlan,
	parent exportParent,
) ([]exportProductRow, error) {
	query, args := exportProductListQuery(input)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query export products: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	products := make([]exportProductRow, 0)
	for rows.Next() {
		row, scanErr := scanExportProductRow(rows.Scan, plan.needsIRODS)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan export product row: %w", ErrUpstreamImpaired, scanErr)
		}
		row.IDStudyLims = parent.Canonical
		if parent.Study != nil {
			row.StudyAccessionNumber = parent.Study.AccessionNumber
		}

		products = append(products, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query export products: %w", ErrUpstreamImpaired, err)
	}

	return products, nil
}

func exportProductListQuery(input exportProductQueryInput) (string, []any) {
	where, whereArgs := exportProductWhere(input)
	selectClause := manifestListSelectPrefix
	join := ""
	args := make([]any, 0)

	if input.needsIRODS {
		irodsJoin, joinArgs, detectMergedCRAMGap := manifestIRODSJoin(input.studyID, input.normalisedFile)
		join = irodsJoin
		args = append(args, joinArgs...)
		selectClause += manifestListIRODSSelect
		if detectMergedCRAMGap {
			selectClause += manifestListIRODSUnmatchedSelect
		} else {
			selectClause += manifestListIRODSNoUnmatchedSelect
		}
	}
	args = append(args, whereArgs...)
	query := selectClause + manifestListBaseFrom + join + where
	if input.cursor.set {
		query += ` AND (ipm.id_run, ipm.position, ipm.tag_index) > (?, ?, ?)`
		args = append(args, input.cursor.IDRun, input.cursor.Position, input.cursor.TagIndex)
	}
	query += ` GROUP BY ipm.id_run, ipm.position, ipm.tag_index`
	if having := exportProductQCHaving(input.filters.QC); having != "" {
		query += ` HAVING ` + having
	}
	query += ` ORDER BY ipm.id_run, ipm.position, ipm.tag_index, MIN(sm.name) LIMIT ?`
	args = append(args, input.limit)
	if input.offset > 0 && !input.cursor.set {
		query += ` OFFSET ?`
		args = append(args, input.offset)
	}

	return query, args
}

func exportProductWhere(input exportProductQueryInput) (string, []any) {
	query := ` WHERE ipm.id_study_lims = ?`
	args := []any{input.studyID}

	if input.filters.LibraryType != "" {
		query += ` AND EXISTS (` +
			`SELECT 1 FROM library_samples ls ` +
			`WHERE ls.id_sample_tmp = ipm.id_sample_tmp ` +
			`AND ls.id_study_lims = ipm.id_study_lims ` +
			`AND ls.pipeline_id_lims = ?` +
			`)`
		args = append(args, input.filters.LibraryType)
	}
	if len(input.organismCommonNames) > 0 {
		query += ` AND sm.common_name IN (` + placeholders(len(input.organismCommonNames)) + `)`
		for _, commonName := range input.organismCommonNames {
			args = append(args, commonName)
		}
	}
	if input.deliverablesOnly {
		query += ` AND EXISTS (` +
			`SELECT 1 FROM iseq_flowcell_mirror ifc ` +
			`WHERE ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp ` +
			`AND ifc.entity_type IN ('library', 'library_indexed')` +
			`)`
	}

	return query, args
}

func exportProductQCHaving(qc string) string {
	pendingCount := `SUM(CASE WHEN ipm.qc IS NULL THEN 1 ELSE 0 END)`

	switch qc {
	case qcFail:
		return `MIN(ipm.qc) = 0`
	case qcPending:
		return `(MIN(ipm.qc) IS NULL OR MIN(ipm.qc) <> 0) AND ` + pendingCount + ` > 0`
	case qcPass:
		return `MIN(ipm.qc) = 1 AND ` + pendingCount + ` = 0`
	default:
		return ""
	}
}

func scanExportProductRow(scan func(dest ...any) error, withIRODS bool) (exportProductRow, error) {
	var (
		row             exportProductRow
		name            sql.NullString
		supplierName    sql.NullString
		accessionNumber sql.NullString
		sangerSampleID  sql.NullString
		productCount    int
		pendingQC       sql.NullInt64
		minQC           sql.NullInt64
		collection      sql.NullString
		fileName        sql.NullString
		unmatched       int
	)

	dest := []any{
		&row.IDRun,
		&row.Position,
		&row.TagIndex,
		&name,
		&supplierName,
		&accessionNumber,
		&sangerSampleID,
		&productCount,
		&pendingQC,
		&minQC,
	}
	if withIRODS {
		dest = append(dest, &collection, &fileName, &unmatched)
	}
	if err := scan(dest...); err != nil {
		return exportProductRow{}, err
	}

	row.Name = nullStringValue(name)
	row.SupplierName = nullStringValue(supplierName)
	row.AccessionNumber = nullStringValue(accessionNumber)
	row.SangerSampleID = nullStringValue(sangerSampleID)
	row.ManualQC = qcRollupString(productCount, pendingQC, minQC)
	if withIRODS && collection.Valid && fileName.Valid {
		row.IRODSPath = strings.TrimRight(collection.String, "/") + "/" + fileName.String
	}
	if withIRODS && unmatched != 0 {
		row.IRODSUnmatched = true
		row.Reason = manifestUnmatchedReasonMergedMultilane
	}

	return row, nil
}

func (c *Client) samplesForExport(ctx context.Context, plan exportPlan, parent exportParent) ([]Sample, int, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	commonNames, err := c.exportOrganismCommonNames(ctx, db, plan.filters.Organism)
	if err != nil {
		return nil, 0, err
	}
	if plan.filters.Organism != "" && len(commonNames) == 0 {
		return []Sample{}, 0, nil
	}

	query, args, err := exportSamplesPageQuery(plan, parent, commonNames)
	if err != nil {
		return nil, 0, err
	}

	rows, err := querySamples(ctx, db, query, "query export samples", args...)
	if err != nil {
		return nil, 0, err
	}

	totalQuery, totalArgs, err := exportSamplesCountQuery(plan, parent, commonNames)
	if err != nil {
		return nil, 0, err
	}
	total, err := c.queryCount(ctx, totalQuery, "count export samples", totalArgs...)
	if err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}

func exportSamplesPageQuery(plan exportPlan, parent exportParent, commonNames []string) (string, []any, error) {
	where, args, err := exportSamplesWhere(plan, parent, commonNames)
	if err != nil {
		return "", nil, err
	}

	query := `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror` + where +
		` ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	args = append(args, plan.limit, plan.offset)

	return query, args, nil
}

func exportSamplesCountQuery(plan exportPlan, parent exportParent, commonNames []string) (string, []any, error) {
	where, args, err := exportSamplesWhere(plan, parent, commonNames)
	if err != nil {
		return "", nil, err
	}

	return `SELECT COUNT(*) FROM sample_mirror` + where, args, nil
}

func exportSamplesWhere(plan exportPlan, parent exportParent, commonNames []string) (string, []any, error) {
	clauses := []string{`sample_mirror.id_lims = 'SQSCP'`}
	args := make([]any, 0)

	parentClause, parentArgs, err := exportSampleParentClause(plan, parent)
	if err != nil {
		return "", nil, err
	}
	clauses = append(clauses, parentClause)
	args = append(args, parentArgs...)

	if len(commonNames) > 0 {
		clauses = append(clauses, `sample_mirror.common_name IN (`+placeholders(len(commonNames))+`)`)
		for _, commonName := range commonNames {
			args = append(args, commonName)
		}
	}
	if plan.filters.LibraryType != "" {
		clause, clauseArgs := exportSampleLibraryTypeClause(plan, parent)
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	if plan.filters.QC != "" {
		clause, clauseArgs := exportSampleQCClause(plan, parent)
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	if plan.deliverablesOnly {
		clause, clauseArgs := exportSampleDeliverableClause(plan, parent)
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}

	return ` WHERE ` + strings.Join(clauses, ` AND `), args, nil
}

func exportSampleParentClause(plan exportPlan, parent exportParent) (string, []any, error) {
	switch plan.rel.ParentKind {
	case "study":
		return `EXISTS (SELECT 1 FROM library_samples parent_ls WHERE parent_ls.id_sample_tmp = sample_mirror.id_sample_tmp AND parent_ls.id_study_lims = ?)`, []any{parent.Canonical}, nil
	case "run":
		return `EXISTS (SELECT 1 FROM iseq_product_metrics_mirror parent_ipm WHERE parent_ipm.id_sample_tmp = sample_mirror.id_sample_tmp AND parent_ipm.id_run = ?)`, []any{parent.Value}, nil
	case "library":
		switch {
		case parent.Library != nil && parent.Library.LibraryID != "":
			return `EXISTS (SELECT 1 FROM library_samples parent_ls WHERE parent_ls.id_sample_tmp = sample_mirror.id_sample_tmp AND parent_ls.library_id = ?)`, []any{parent.Library.LibraryID}, nil
		case parent.Library != nil && parent.Library.IDLibraryLims != "":
			return `EXISTS (SELECT 1 FROM library_samples parent_ls WHERE parent_ls.id_sample_tmp = sample_mirror.id_sample_tmp AND parent_ls.id_library_lims = ?)`, []any{parent.Library.IDLibraryLims}, nil
		default:
			return `EXISTS (SELECT 1 FROM library_samples parent_ls WHERE parent_ls.id_sample_tmp = sample_mirror.id_sample_tmp AND parent_ls.pipeline_id_lims = ?)`, []any{parent.Canonical}, nil
		}
	default:
		return "", nil, ErrUnsupportedIdentifier
	}
}

func exportSampleLibraryTypeClause(plan exportPlan, parent exportParent) (string, []any) {
	args := []any{plan.filters.LibraryType}
	clause := `EXISTS (SELECT 1 FROM library_samples filter_ls WHERE filter_ls.id_sample_tmp = sample_mirror.id_sample_tmp AND filter_ls.pipeline_id_lims = ?`
	if plan.rel.ParentKind == "study" {
		clause += ` AND filter_ls.id_study_lims = ?`
		args = append(args, parent.Canonical)
	}
	clause += `)`

	return clause, args
}

func exportSampleQCClause(plan exportPlan, parent exportParent) (string, []any) {
	return exportSampleProductExistsClause(plan, parent, exportRawQCPredicate(plan.filters.QC))
}

func exportRawQCPredicate(qc string) string {
	switch qc {
	case qcPass:
		return `pm.qc = 1`
	case qcFail:
		return `pm.qc = 0`
	default:
		return `pm.qc IS NULL`
	}
}

func exportSampleProductExistsClause(plan exportPlan, parent exportParent, predicate string) (string, []any) {
	tables := []string{
		"iseq_product_metrics_mirror",
		"pac_bio_product_metrics_mirror",
		"eseq_product_metrics_mirror",
		"useq_product_metrics_mirror",
	}
	arms := make([]string, 0, len(tables))
	args := make([]any, 0, len(tables))
	for _, table := range tables {
		if plan.rel.ParentKind == "run" && table == "pac_bio_product_metrics_mirror" {
			continue
		}

		arm := `SELECT 1 FROM ` + table + ` pm WHERE pm.id_sample_tmp = sample_mirror.id_sample_tmp`
		switch plan.rel.ParentKind {
		case "study":
			arm += ` AND pm.id_study_lims = ?`
			args = append(args, parent.Canonical)
		case "run":
			arm += ` AND pm.id_run = ?`
			args = append(args, parent.Value)
		case "library":
			if parent.Library != nil && parent.Library.IDStudyLims != "" {
				arm += ` AND pm.id_study_lims = ?`
				args = append(args, parent.Library.IDStudyLims)
			}
		}
		arm += ` AND ` + predicate
		arms = append(arms, arm)
	}

	return `EXISTS (` + strings.Join(arms, ` UNION ALL `) + `)`, args
}

func exportSampleDeliverableClause(plan exportPlan, parent exportParent) (string, []any) {
	arms := make([]string, 0, 5)
	args := make([]any, 0, 5)
	addScopedArm := func(table, predicate string, supportsRun bool) {
		if plan.rel.ParentKind == "run" && !supportsRun {
			return
		}

		arm := `SELECT 1 FROM ` + table + ` pm WHERE pm.id_sample_tmp = sample_mirror.id_sample_tmp`
		switch plan.rel.ParentKind {
		case "study":
			arm += ` AND pm.id_study_lims = ?`
			args = append(args, parent.Canonical)
		case "run":
			arm += ` AND pm.id_run = ?`
			args = append(args, parent.Value)
		case "library":
			if parent.Library != nil && parent.Library.IDStudyLims != "" {
				arm += ` AND pm.id_study_lims = ?`
				args = append(args, parent.Library.IDStudyLims)
			}
		}
		arm += ` AND ` + predicate
		arms = append(arms, arm)
	}

	illuminaArm := `SELECT 1 FROM iseq_product_metrics_mirror pm INNER JOIN iseq_flowcell_mirror ifc ON ifc.id_iseq_flowcell_tmp = pm.id_iseq_flowcell_tmp WHERE pm.id_sample_tmp = sample_mirror.id_sample_tmp`
	switch plan.rel.ParentKind {
	case "study":
		illuminaArm += ` AND pm.id_study_lims = ?`
		args = append(args, parent.Canonical)
	case "run":
		illuminaArm += ` AND pm.id_run = ?`
		args = append(args, parent.Value)
	case "library":
		if parent.Library != nil && parent.Library.IDStudyLims != "" {
			illuminaArm += ` AND pm.id_study_lims = ?`
			args = append(args, parent.Library.IDStudyLims)
		}
	}
	illuminaArm += ` AND ifc.entity_type IN ('library', 'library_indexed')`
	arms = append(arms, illuminaArm)

	addScopedArm("eseq_product_metrics_mirror", `pm.is_sequencing_control = 0`, true)
	addScopedArm("useq_product_metrics_mirror", `pm.is_sequencing_control = 0`, true)
	addScopedArm("pac_bio_product_metrics_mirror", `1 = 1`, false)
	if plan.rel.ParentKind != "run" {
		ontArm := `SELECT 1 FROM oseq_flowcell_mirror pm WHERE pm.id_sample_tmp = sample_mirror.id_sample_tmp`
		if plan.rel.ParentKind == "study" {
			ontArm += ` AND pm.id_study_lims = ?`
			args = append(args, parent.Canonical)
		} else if parent.Library != nil && parent.Library.IDStudyLims != "" {
			ontArm += ` AND pm.id_study_lims = ?`
			args = append(args, parent.Library.IDStudyLims)
		}
		arms = append(arms, ontArm)
	}

	return `EXISTS (` + strings.Join(arms, ` UNION ALL `) + `)`, args
}

func (c *Client) runRowsForExport(ctx context.Context, plan exportPlan, parent exportParent) ([]exportRunRow, int, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	query, countQuery, args, countArgs, err := exportRunQueries(plan, parent, c.runListingDialect())
	if err != nil {
		return nil, 0, err
	}

	rows, err := c.queryExportRunRows(ctx, db, query, args...)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		if syncErr := c.requireAnySyncState(ctx, exportRunSyncTables()...); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []exportRunRow{}, 0, syncErr
			}

			return nil, 0, syncErr
		}
	}
	if err = validateExportRunColumns(rows, plan.columns); err != nil {
		return nil, 0, err
	}

	total, err := c.queryCount(ctx, countQuery, "count export run rows", countArgs...)
	if err != nil {
		return nil, 0, err
	}

	return rows, total, nil
}

func exportRunSyncTables() []string {
	tables := syncTablesForRunAggregationSpecs(runAggregationPlatformSpecs)
	tables = append(tables, sequencingAggregateProductSyncTables(runAggregationPlatformSpecs)...)

	return uniqueStrings(tables)
}

func exportRunQueries(plan exportPlan, parent exportParent, dialect string) (string, string, []any, []any, error) {
	union, args, err := exportRunUnionQuery(plan, parent, dialect)
	if err != nil {
		return "", "", nil, nil, err
	}

	query := `SELECT platform_key, platform, native_id, id_run, manufacturer, run_date, date_basis FROM (` + union + `) AS runs ` +
		`ORDER BY platform_key, native_id LIMIT ? OFFSET ?`
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, plan.limit, plan.offset)
	countQuery := `SELECT COUNT(*) FROM (` + union + `) AS runs`
	countArgs := append([]any{}, args...)

	return query, countQuery, queryArgs, countArgs, nil
}

func exportRunUnionQuery(plan exportPlan, parent exportParent, dialect string) (string, []any, error) {
	arms := make([]string, 0, len(runAggregationPlatformSpecs))
	args := make([]any, 0, len(runAggregationPlatformSpecs))
	for _, spec := range runAggregationPlatformSpecs {
		arm, armArgs, err := exportRunArmQuery(spec, plan, parent, dialect)
		if err != nil {
			return "", nil, err
		}
		arms = append(arms, arm)
		args = append(args, armArgs...)
	}

	return strings.Join(arms, " UNION ALL "), args, nil
}

func exportRunArmQuery(spec runAggregationPlatformSpec, plan exportPlan, parent exportParent, dialect string) (string, []any, error) {
	where, value, err := exportRunParentScope(plan, parent)
	if err != nil {
		return "", nil, err
	}

	numericID := func(expr string) string {
		return runListingIntegerTextExpr(dialect, expr)
	}
	args := []any{value}

	switch spec.platform {
	case platformIllumina:
		runDate := `MIN(CASE WHEN status_dict.description = '` + runDateBasisRunComplete + `' THEN status.normalised_date ELSE NULL END)`
		scopedRuns := `SELECT DISTINCT id_run FROM iseq_product_metrics_mirror WHERE ` + where
		query := `SELECT 'illumina' AS platform_key, '` + platformIllumina + `' AS platform, ` +
			numericID("run_ids.id_run") + ` AS native_id, ` + numericID("run_ids.id_run") + ` AS id_run, '` +
			platformIllumina + `' AS manufacturer, COALESCE(` + runDate + `, '') AS run_date, ` +
			`CASE WHEN ` + runDate + ` IS NULL THEN '' ELSE '` + runDateBasisRunComplete + `' END AS date_basis ` +
			`FROM (` + scopedRuns + `) AS run_ids ` +
			`LEFT JOIN iseq_run_status_mirror AS status ON status.id_run = run_ids.id_run ` +
			`LEFT JOIN iseq_run_status_dict_mirror AS status_dict ON status_dict.id_run_status_dict = status.id_run_status_dict ` +
			`GROUP BY run_ids.id_run`

		return query, args, nil
	case platformPacBio:
		runDate := `MIN(NULLIF(pb.normalised_date, ''))`
		query := `SELECT 'pacbio' AS platform_key, '` + platformPacBio + `' AS platform, pb.pac_bio_run_name AS native_id, ` +
			`'' AS id_run, '` + platformPacBio + `' AS manufacturer, ` + runDate + ` AS run_date, ` +
			`CASE WHEN ` + runDate + ` IS NULL THEN '' ELSE '` + runDateBasisPacBioComplete + `' END AS date_basis ` +
			`FROM pac_bio_product_metrics_mirror AS pm ` +
			`INNER JOIN pac_bio_run_well_metrics_mirror AS pb ON pb.id_pac_bio_rw_metrics_tmp = pm.id_pac_bio_rw_metrics_tmp ` +
			`WHERE pm.` + where + ` GROUP BY pb.pac_bio_run_name`

		return query, args, nil
	case platformElembio:
		runDate := `MIN(NULLIF(er.normalised_date, ''))`
		query := `SELECT 'elembio' AS platform_key, '` + platformElembio + `' AS platform, ` + numericID("er.id_run") + ` AS native_id, ` +
			numericID("er.id_run") + ` AS id_run, '` + runManufacturerElembio + `' AS manufacturer, ` + runDate + ` AS run_date, ` +
			`CASE WHEN ` + runDate + ` IS NULL THEN '' ELSE '` + runDateBasisRunComplete + `' END AS date_basis ` +
			`FROM eseq_product_metrics_mirror AS pm ` +
			`INNER JOIN eseq_run_lane_metrics_mirror AS er ON er.id_run = pm.id_run ` +
			`WHERE pm.` + where + ` GROUP BY er.id_run`

		return query, args, nil
	case platformUltimagen:
		runDate := `MIN(NULLIF(ur.normalised_date, ''))`
		query := `SELECT 'ultimagen' AS platform_key, '` + platformUltimagen + `' AS platform, ` + numericID("ur.id_run") + ` AS native_id, ` +
			numericID("ur.id_run") + ` AS id_run, '` + runManufacturerUltimagen + `' AS manufacturer, ` + runDate + ` AS run_date, ` +
			`CASE WHEN ` + runDate + ` IS NULL THEN '' ELSE '` + runDateBasisRunArchived + `' END AS date_basis ` +
			`FROM useq_product_metrics_mirror AS pm ` +
			`INNER JOIN useq_run_metrics_mirror AS ur ON ur.id_run = pm.id_run ` +
			`WHERE pm.` + where + ` GROUP BY ur.id_run`

		return query, args, nil
	case platformONT:
		runDate := `MIN(NULLIF(ont.normalised_date, ''))`
		query := `SELECT 'ont' AS platform_key, '` + platformONT + `' AS platform, ont.experiment_name AS native_id, ` +
			`'' AS id_run, '` + runManufacturerONT + `' AS manufacturer, ` + runDate + ` AS run_date, ` +
			`CASE WHEN ` + runDate + ` IS NULL THEN '' ELSE '` + runDateBasisONTLoadTime + `' END AS date_basis FROM oseq_flowcell_mirror AS ont ` +
			`WHERE ont.` + where + ` GROUP BY ont.experiment_name`

		return query, args, nil
	default:
		return "", nil, fmt.Errorf("%w: unsupported platform %q", ErrUnsupportedIdentifier, spec.platform)
	}
}

func exportRunParentScope(plan exportPlan, parent exportParent) (string, any, error) {
	switch plan.rel.ParentKind {
	case "study":
		return "id_study_lims = ?", parent.Canonical, nil
	case "sample":
		return "id_sample_tmp = ?", parent.Value, nil
	default:
		return "", nil, ErrUnsupportedIdentifier
	}
}

func (c *Client) streamExportPages(ctx context.Context, plan exportPlan, parent exportParent, emit func([]string) error) (int, error) {
	pagePlan := plan
	pagePlan.all = false
	pagePlan.cursor = exportCursor{}

	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}

		page, err := c.exportPage(ctx, pagePlan, parent)
		if err != nil {
			return count, err
		}
		emitted, err := page.ForEachRow(ctx, emit)
		count += emitted
		if err != nil || page.Complete {
			return count, err
		}
		if emitted == 0 {
			return count, fmt.Errorf("%w: export did not advance while paging all rows", ErrUpstreamImpaired)
		}

		pagePlan.offset += emitted
	}
}

type exportCursor struct {
	IDRun                int64
	Position             int64
	TagIndex             int64
	IDSeqProductLocation int64
	set                  bool
}

type exportProductQueryInput struct {
	studyID             string
	needsIRODS          bool
	normalisedFile      string
	deliverablesOnly    bool
	filters             exportFilters
	organismCommonNames []string
	limit               int
	offset              int
	cursor              exportCursor
}

func (c *Client) exportProductTotal(ctx context.Context, input exportProductQueryInput) (int, error) {
	if input.filters == (exportFilters{}) && !input.deliverablesOnly && len(input.organismCommonNames) == 0 {
		return c.countStudyManifestProducts(ctx, input.studyID)
	}

	query, args := exportProductCountQuery(input)

	return c.queryCount(ctx, query, "count export products", args...)
}

func exportProductCountQuery(input exportProductQueryInput) (string, []any) {
	where, args := exportProductWhere(input)
	groupBy := ` GROUP BY ipm.id_run, ipm.position, ipm.tag_index`

	if having := exportProductQCHaving(input.filters.QC); having != "" {
		query := `SELECT COUNT(*) FROM (` +
			`SELECT ipm.id_run, ipm.position, ipm.tag_index` +
			manifestListBaseFrom + where + groupBy + ` HAVING ` + having +
			`) AS export_products`

		return query, args
	}

	query := `SELECT COUNT(*) FROM (` +
		`SELECT DISTINCT ipm.id_run, ipm.position, ipm.tag_index` +
		manifestListBaseFrom + where +
		`) AS export_products`

	return query, args
}

type exportPlan struct {
	rel               ExportRelationship
	kind              exportRelationshipKind
	columns           []exportColumn
	format            string
	needsIRODS        bool
	normalisedFile    string
	deliverablesOnly  bool
	role              string
	filters           exportFilters
	limit             int
	offset            int
	all               bool
	cursor            exportCursor
	sortCreatedDesc   bool
	createdWindowArgs []any
}

func (c *Client) streamExportIRODSRows(ctx context.Context, db *sql.DB, plan exportPlan, input exportIRODSQueryInput, emit func([]string) error) (int, error) {
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}

		input.limit = plan.limit
		page, err := c.queryExportIRODSRows(ctx, db, input)
		if err != nil {
			return count, err
		}
		if len(page) == 0 {
			return count, nil
		}

		for _, row := range projectExportIRODSRows(page, plan.columns) {
			if err := emit(row); err != nil {
				return count, err
			}
			count++
		}
		if len(page) < plan.limit {
			return count, nil
		}

		if plan.sortCreatedDesc {
			input.cursor = exportCursor{}
			input.offset += len(page)
		} else {
			input.cursor = page[len(page)-1].cursor()
			input.offset = 0
		}
	}
}

type exportStudyRows struct {
	rows  [][]string
	total int
}

func (c *Client) studiesForExport(ctx context.Context, plan exportPlan, parent exportParent) (exportStudyRows, error) {
	switch plan.rel.ParentKind {
	case "sample":
		studies, total, err := c.sampleStudiesForExport(ctx, parent.Canonical, plan.limit, plan.offset)
		if err != nil {
			return exportStudyRows{}, err
		}

		return exportStudyRows{rows: projectStudies(studies, "", plan.columns), total: total}, nil
	case "faculty-sponsor":
		rows, err := c.StudiesForFacultySponsor(ctx, parent.Canonical, plan.limit, plan.offset)
		total, countErr := c.CountStudiesForFacultySponsor(ctx, parent.Canonical)
		if err = errors.Join(err, countErr); err != nil {
			return exportStudyRows{}, err
		}

		return exportStudyRows{rows: projectPersonStudies(rows, plan.columns), total: exportCountValue(total)}, nil
	case "user":
		rows, err := c.StudiesForUser(ctx, parent.Canonical, plan.role, plan.limit, plan.offset)
		total, countErr := c.CountStudiesForUser(ctx, parent.Canonical, plan.role)
		if err = errors.Join(err, countErr); err != nil {
			return exportStudyRows{}, err
		}

		return exportStudyRows{rows: projectPersonStudies(rows, plan.columns), total: exportCountValue(total)}, nil
	case "programme":
		rows, total, err := c.programmeStudiesForExport(ctx, parent.Canonical, plan.limit, plan.offset)
		if err != nil {
			return exportStudyRows{}, err
		}

		return exportStudyRows{rows: projectStudies(rows, "", plan.columns), total: total}, nil
	default:
		return exportStudyRows{}, ErrUnsupportedIdentifier
	}
}

type exportRunRow struct {
	ID           string
	NativeID     string
	IDRun        string
	Platform     string
	Manufacturer string
	RunDate      string
	DateBasis    string
}

func (c *Client) queryExportRunRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]exportRunRow, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query export run rows: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	exportRows := make([]exportRunRow, 0)
	for rows.Next() {
		var (
			platformKey string
			row         exportRunRow
			runDate     sql.NullString
		)
		if err = rows.Scan(&platformKey, &row.Platform, &row.NativeID, &row.IDRun, &row.Manufacturer, &runDate, &row.DateBasis); err != nil {
			return nil, fmt.Errorf("%w: scan export run row: %w", ErrUpstreamImpaired, err)
		}

		row.ID = platformKey + ":" + row.NativeID
		row.RunDate = nullStringValue(runDate)
		exportRows = append(exportRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query export run rows: %w", ErrUpstreamImpaired, err)
	}

	return exportRows, nil
}

type exportIRODSQueryInput struct {
	parentKind          string
	parentValue         any
	columns             []exportColumn
	normalisedFile      string
	deliverablesOnly    bool
	filters             exportFilters
	organismCommonNames []string
	limit               int
	offset              int
	cursor              exportCursor
	sortCreatedDesc     bool
	createdWindowArgs   []any
}

func (c *Client) exportIRODSTotal(ctx context.Context, db *sql.DB, input exportIRODSQueryInput) (int, error) {
	query, args, err := exportIRODSCountQuery(input)
	if err != nil {
		return 0, err
	}

	return c.queryCount(ctx, query, "count export irods rows", args...)
}

func exportIRODSCountQuery(input exportIRODSQueryInput) (string, []any, error) {
	fromWhere, args, err := exportIRODSFromWhere(input, true)
	if err != nil {
		return "", nil, err
	}

	return `SELECT COUNT(*)` + fromWhere, args, nil
}

func (c *Client) queryExportIRODSRows(ctx context.Context, db *sql.DB, input exportIRODSQueryInput) ([]exportIRODSRow, error) {
	query, args, err := exportIRODSPageQuery(input)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query export irods rows: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	exports := make([]exportIRODSRow, 0)
	for rows.Next() {
		row, scanErr := scanExportIRODSRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan export irods row: %w", ErrUpstreamImpaired, scanErr)
		}

		exports = append(exports, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query export irods rows: %w", ErrUpstreamImpaired, err)
	}

	if len(exports) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []exportIRODSRow{}, syncErr
			}

			return nil, syncErr
		}
	}

	return exports, nil
}

func scanExportIRODSRow(scan func(dest ...any) error) (exportIRODSRow, error) {
	var (
		row             exportIRODSRow
		created         sql.NullString
		merged          sql.NullInt64
		name            sql.NullString
		supplierName    sql.NullString
		sangerSampleID  sql.NullString
		sampleAccession sql.NullString
		studyAccession  sql.NullString
	)

	if err := scan(
		&row.IDSeqProductLocation,
		&row.IDProduct,
		&row.Collection,
		&row.FileName,
		&row.IDSampleTmp,
		&row.IDStudyLims,
		&created,
		&row.Platform,
		&row.IDRun,
		&row.Position,
		&row.TagIndex,
		&row.QC,
		&row.IsDeliverable,
		&merged,
		&name,
		&supplierName,
		&sangerSampleID,
		&sampleAccession,
		&studyAccession,
	); err != nil {
		return exportIRODSRow{}, err
	}

	row.Created = nullStringValue(created)
	row.Merged = merged.Valid && merged.Int64 != 0
	row.Name = nullStringValue(name)
	row.SupplierName = nullStringValue(supplierName)
	row.SangerSampleID = nullStringValue(sangerSampleID)
	row.SampleAccession = nullStringValue(sampleAccession)
	row.StudyAccession = nullStringValue(studyAccession)

	return row, nil
}

func exportIRODSPageQuery(input exportIRODSQueryInput) (string, []any, error) {
	fromWhere, args, err := exportIRODSFromWhere(input, false)
	if err != nil {
		return "", nil, err
	}

	query := exportIRODSSelect(input) + fromWhere
	if input.cursor.set {
		query += ` AND (spi.id_run, spi.position, spi.tag_index, spi.id_seq_product_irods_locations_tmp) > (?, ?, ?, ?)`
		args = append(args, input.cursor.IDRun, input.cursor.Position, input.cursor.TagIndex, input.cursor.IDSeqProductLocation)
	}
	if input.sortCreatedDesc {
		query += ` ORDER BY spi.created DESC, spi.id_run, spi.position, spi.tag_index, spi.id_seq_product_irods_locations_tmp LIMIT ?`
	} else {
		query += ` ORDER BY spi.id_run, spi.position, spi.tag_index, spi.id_seq_product_irods_locations_tmp LIMIT ?`
	}
	args = append(args, input.limit)
	if input.offset > 0 && !input.cursor.set {
		query += ` OFFSET ?`
		args = append(args, input.offset)
	}

	return query, args, nil
}

func exportIRODSFromWhere(input exportIRODSQueryInput, count bool) (string, []any, error) {
	query := ` FROM seq_product_irods_locations_mirror spi`
	if input.parentKind == "run" {
		query += ` LEFT JOIN iseq_product_metrics_mirror ipm ON ipm.id_iseq_product = spi.id_iseq_product`
	}
	if exportNeedsSample(input) {
		query += ` LEFT JOIN sample_mirror sm ON sm.id_sample_tmp = spi.id_sample_tmp`
	}
	if exportColumnsNeedStudy(input.columns) {
		query += ` LEFT JOIN study_mirror ON study_mirror.id_study_lims = spi.id_study_lims AND study_mirror.id_lims = 'SQSCP'`
	}

	where, args, err := exportIRODSWhere(input, count)
	if err != nil {
		return "", nil, err
	}

	return query + where, args, nil
}

func exportIRODSSelect(input exportIRODSQueryInput) string {
	sampleName, supplierName, sangerSampleID, sampleAccession := `''`, `''`, `''`, `''`
	if exportNeedsSample(input) {
		sampleName = `COALESCE(sm.name, '')`
		supplierName = `COALESCE(sm.supplier_name, '')`
		sangerSampleID = `COALESCE(sm.sanger_sample_id, '')`
		sampleAccession = `COALESCE(sm.accession_number, '')`
	}

	studyAccession := `''`
	if exportColumnsNeedStudy(input.columns) {
		studyAccession = `COALESCE(study_mirror.accession_number, '')`
	}

	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_iseq_product, spi.irods_collection, spi.irods_file_name, ` +
		`spi.id_sample_tmp, spi.id_study_lims, COALESCE(spi.created, ''), spi.platform, spi.id_run, spi.position, ` +
		`spi.tag_index, spi.qc, spi.is_deliverable, spi.merged, ` + sampleName + `, ` + supplierName + `, ` +
		sangerSampleID + `, ` + sampleAccession + `, ` + studyAccession
}

func exportNeedsSample(input exportIRODSQueryInput) bool {
	return len(input.organismCommonNames) > 0 || exportColumnsNeedSample(input.columns)
}

func exportIRODSWhere(input exportIRODSQueryInput, count bool) (string, []any, error) {
	query, args, err := exportIRODSParentWhere(input.parentKind, input.parentValue)
	if err != nil {
		return "", nil, err
	}
	if input.normalisedFile != "" {
		query += ` AND LOWER(spi.irods_file_name) LIKE ?`
		args = append(args, irodsFileTypeLikePattern(input.normalisedFile))
	}
	if input.deliverablesOnly {
		query += ` AND (spi.is_deliverable = 1 OR spi.is_deliverable IS NULL)`
	}
	if len(input.createdWindowArgs) > 0 {
		query += ` AND spi.created >= ? AND spi.created < ?`
		args = append(args, input.createdWindowArgs...)
	}
	query, args = appendExportIRODSQCWhere(query, args, input.filters.QC)
	if input.filters.LibraryType != "" {
		query += ` AND EXISTS (SELECT 1 FROM library_samples ls WHERE ls.id_sample_tmp = spi.id_sample_tmp AND ls.pipeline_id_lims = ?`
		args = append(args, input.filters.LibraryType)
		if input.parentKind == "study" {
			query += ` AND ls.id_study_lims = spi.id_study_lims`
		}
		query += `)`
	}
	if len(input.organismCommonNames) > 0 {
		query += ` AND sm.common_name IN (` + placeholders(len(input.organismCommonNames)) + `)`
		for _, commonName := range input.organismCommonNames {
			args = append(args, commonName)
		}
	}
	if count {
		return query, args, nil
	}

	return query, args, nil
}

func exportIRODSParentWhere(parentKind string, parentValue any) (string, []any, error) {
	switch parentKind {
	case "study":
		return ` WHERE spi.id_study_lims = ?`, []any{parentValue}, nil
	case "sample":
		return ` WHERE spi.id_sample_tmp = ?`, []any{parentValue}, nil
	case "run":
		return ` WHERE ` + irodsRunScopePredicate, []any{parentValue}, nil
	default:
		return "", nil, fmt.Errorf("%w: unsupported iRODS export parent %q", ErrUnsupportedIdentifier, parentKind)
	}
}

type exportIRODSRow struct {
	IDSeqProductLocation int64
	IDProduct            string
	Collection           string
	FileName             string
	IDSampleTmp          int64
	IDStudyLims          string
	Created              string
	Platform             string
	IDRun                int64
	Position             int64
	TagIndex             int64
	QC                   sql.NullInt64
	IsDeliverable        sql.NullInt64
	Merged               bool
	Name                 string
	SupplierName         string
	SangerSampleID       string
	SampleAccession      string
	StudyAccession       string
}

func (row exportIRODSRow) cell(column string) string {
	switch column {
	case "supplier_name":
		return row.SupplierName
	case "sanger_sample_id":
		return row.SangerSampleID
	case "name":
		return row.Name
	case "accession_number":
		return row.SampleAccession
	case "study_accession_number":
		return row.StudyAccession
	case "id_study_lims":
		return row.IDStudyLims
	case "manual_qc":
		if strings.EqualFold(row.Platform, "ont") {
			return ""
		}

		return qcString(row.QC)
	case "id_run":
		if row.Merged {
			return "0"
		}

		return strconv.FormatInt(row.IDRun, 10)
	case "lane":
		if row.Merged {
			return "0"
		}

		return strconv.FormatInt(row.Position, 10)
	case "tag_index":
		if row.Merged {
			return "0"
		}

		return strconv.FormatInt(row.TagIndex, 10)
	case "platform":
		return row.Platform
	case "created":
		return row.Created
	case "merged":
		return strconv.FormatBool(row.Merged)
	case "deliverable":
		return deliverableString(row.IsDeliverable)
	case "irods_path":
		return row.IRODSPath()
	case "id_product":
		return row.IDProduct
	case "id_sample_tmp":
		return strconv.FormatInt(row.IDSampleTmp, 10)
	case "collection":
		return row.Collection
	case "data_object":
		return row.FileName
	default:
		return ""
	}
}

func deliverableString(value sql.NullInt64) string {
	if !value.Valid {
		return ""
	}
	if value.Int64 == 0 {
		return "false"
	}

	return "true"
}

func (row exportIRODSRow) cursor() exportCursor {
	return exportCursor{
		IDRun:                row.IDRun,
		Position:             row.Position,
		TagIndex:             row.TagIndex,
		IDSeqProductLocation: row.IDSeqProductLocation,
		set:                  true,
	}
}

type exportProductRow struct {
	IDRun, Position, TagIndex         int
	Name, SupplierName                string
	AccessionNumber, SangerSampleID   string
	ManualQC                          string
	IRODSPath                         string
	IRODSUnmatched                    bool
	Reason                            string
	IDStudyLims, StudyAccessionNumber string
}

func (row exportProductRow) cell(column string) string {
	switch column {
	case "name":
		return row.Name
	case "supplier_name":
		return row.SupplierName
	case "accession_number":
		return row.AccessionNumber
	case "sanger_sample_id":
		return row.SangerSampleID
	case "id_run":
		return strconv.Itoa(row.IDRun)
	case "lane":
		return strconv.Itoa(row.Position)
	case "tag_index":
		return strconv.Itoa(row.TagIndex)
	case "manual_qc":
		return row.ManualQC
	case "irods_path":
		return row.IRODSPath
	case "irods_unmatched":
		return strconv.FormatBool(row.IRODSUnmatched)
	case "reason":
		return row.Reason
	case "id_study_lims":
		return row.IDStudyLims
	case "study_accession_number":
		return row.StudyAccessionNumber
	default:
		return ""
	}
}

func (row exportProductRow) cursor() exportCursor {
	return exportCursor{
		IDRun:                int64(row.IDRun),
		Position:             int64(row.Position),
		TagIndex:             int64(row.TagIndex),
		IDSeqProductLocation: 0,
		set:                  true,
	}
}

func (c *Client) sampleStudiesForExport(ctx context.Context, sangerName string, limit, offset int) ([]Study, int, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := c.queryStudySearch(ctx, db, studiesForSampleCacheSQL+" LIMIT ? OFFSET ?", sangerName, limit, offset)
	total, countErr := c.CountStudiesForSample(ctx, sangerName)
	if err = errors.Join(err, countErr); err != nil {
		return nil, 0, err
	}

	return rows, exportCountValue(total), nil
}

func (c *Client) resolveExportStudy(ctx context.Context, parentID string) (Match, error) {
	match, err := c.ResolveStudy(ctx, parentID)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return match, err
	}

	study, exactErr := c.resolveStudyFromCache(
		ctx,
		`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
		parentID,
	)
	if exactErr != nil {
		return Match{}, err
	}

	return Match{Kind: KindStudyLimsID, Canonical: study.IDStudyLims, Study: study}, nil
}

func (c *Client) exportOrganismCommonNames(ctx context.Context, db *sql.DB, organism string) ([]string, error) {
	if strings.TrimSpace(organism) == "" {
		return nil, nil
	}

	return c.resolveOrganismCommonNames(ctx, db, organism)
}

func (c *Client) programmeStudiesForExport(ctx context.Context, programme string, limit, offset int) ([]Study, int, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	studies, err := c.queryStudySearch(ctx, db, studiesForProgrammeD1cSQL(c.cacheDialect()), programme, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	total, err := c.queryCount(ctx, countStudiesForProgrammeD1cSQL(c.cacheDialect()), "count programme studies", programme)
	if err != nil {
		return nil, 0, err
	}

	return studies, total, nil
}
