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

package seqmeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wtsi-hgi/wa/mlwh"
)

// PreparedDiff holds a computed diff and a deferred commit step.
type PreparedDiff[T any] struct {
	Result          *DiffResult[T]
	store           *Store
	queryKey        string
	previousEntries map[string]StoredEntry
	nextEntries     map[string]StoredEntry
}

// Commit persists the prepared watermark state.
func (d *PreparedDiff[T]) Commit() error {
	if d == nil {
		return nil
	}

	return d.save(d.nextEntries)
}

// Rollback restores the watermark state captured before Commit.
func (d *PreparedDiff[T]) Rollback() error {
	if d == nil {
		return nil
	}

	return d.save(d.previousEntries)
}

func (d *PreparedDiff[T]) save(entries map[string]StoredEntry) error {
	if d == nil {
		return nil
	}

	return d.store.SaveEntries(d.queryKey, entries)
}

// Diff computes the change set for one logical query.
func Diff[T any](
	store *Store,
	queryKey string,
	current []T,
	idFunc func(T) string,
) (*DiffResult[T], error) {
	var result *DiffResult[T]

	err := store.WithLock(func() error {
		prepared, err := PrepareDiff(store, queryKey, current, idFunc)
		if err != nil {
			return err
		}

		if err := prepared.Commit(); err != nil {
			return err
		}

		result = prepared.Result

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// DiffStudies fetches and diffs the full study list.
func DiffStudies(
	ctx context.Context,
	provider Provider,
	store *Store,
) (*DiffResult[mlwh.Study], error) {
	studies, err := listAllStudies(ctx, provider)
	if err != nil {
		return nil, err
	}

	var result *DiffResult[mlwh.Study]

	err = store.WithLock(func() error {
		prepared, err := prepareDiffStudies(store, studies)
		if err != nil {
			return err
		}

		if err := prepared.Commit(); err != nil {
			return err
		}

		result = prepared.Result

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// PrepareDiffStudies fetches and computes a deferred diff for the full study list.
func PrepareDiffStudies(
	ctx context.Context,
	provider Provider,
	store *Store,
) (*PreparedDiff[mlwh.Study], error) {
	studies, err := listAllStudies(ctx, provider)
	if err != nil {
		return nil, err
	}

	return prepareDiffStudies(store, studies)
}

func prepareDiffStudies(
	store *Store,
	studies []mlwh.Study,
) (*PreparedDiff[mlwh.Study], error) {
	return PrepareDiff(store, "studies:all", studies, func(study mlwh.Study) string {
		return study.IDStudyLims
	})
}

// PrepareDiff computes a diff and returns a deferred commit step.
func PrepareDiff[T any](
	store *Store,
	queryKey string,
	current []T,
	idFunc func(T) string,
) (*PreparedDiff[T], error) {
	previous, err := store.LoadEntries(queryKey)
	if err != nil {
		return nil, err
	}

	result := &DiffResult[T]{
		Added:    []T{},
		Modified: []T{},
		Removed:  []string{},
	}

	type groupedItems struct {
		items []T
		hash  string
	}

	currentGroups := map[string]groupedItems{}
	groupOrder := make([]string, 0)
	for _, item := range current {
		entryID := idFunc(item)
		if _, exists := currentGroups[entryID]; !exists {
			groupOrder = append(groupOrder, entryID)
		}

		group := currentGroups[entryID]
		group.items = append(group.items, item)
		currentGroups[entryID] = group
	}

	for _, entryID := range groupOrder {
		group := currentGroups[entryID]
		hash, err := hashItems(group.items)
		if err != nil {
			return nil, err
		}

		group.hash = hash
		currentGroups[entryID] = group
	}

	now := time.Now().UTC()
	nextEntries := make(map[string]StoredEntry, len(previous)+len(currentGroups))
	for entryID, entry := range previous {
		nextEntries[entryID] = entry
	}

	for _, entryID := range groupOrder {
		group := currentGroups[entryID]
		previousEntry, exists := previous[entryID]

		switch {
		case !exists || previousEntry.Tombstone:
			result.Added = append(result.Added, group.items...)
		case previousEntry.EntryHash != group.hash:
			result.Modified = append(result.Modified, group.items...)
		}

		nextEntries[entryID] = StoredEntry{
			EntryHash: group.hash,
			Tombstone: false,
			UpdatedAt: now,
		}
	}

	for entryID, previousEntry := range previous {
		if _, exists := currentGroups[entryID]; exists {
			continue
		}

		if previousEntry.Tombstone {
			nextEntries[entryID] = previousEntry

			continue
		}

		result.Removed = append(result.Removed, entryID)
		nextEntries[entryID] = StoredEntry{
			EntryHash: previousEntry.EntryHash,
			Tombstone: true,
			UpdatedAt: now,
		}
	}

	sort.Strings(result.Removed)

	return &PreparedDiff[T]{
		Result:          result,
		store:           store,
		queryKey:        queryKey,
		previousEntries: previous,
		nextEntries:     nextEntries,
	}, nil
}

// DiffStudySamples fetches and diffs the samples for one study.
func DiffStudySamples(
	ctx context.Context,
	provider Provider,
	store *Store,
	studyID string,
) (*DiffResult[mlwh.Sample], error) {
	samples, err := listStudySamples(ctx, provider, studyID)
	if err != nil {
		return nil, err
	}

	var result *DiffResult[mlwh.Sample]

	err = store.WithLock(func() error {
		prepared, err := prepareDiffStudySamples(store, studyID, samples)
		if err != nil {
			return err
		}

		if err := prepared.Commit(); err != nil {
			return err
		}

		result = prepared.Result

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// PrepareDiffStudySamples fetches and computes a deferred diff for one study.
func PrepareDiffStudySamples(
	ctx context.Context,
	provider Provider,
	store *Store,
	studyID string,
) (*PreparedDiff[mlwh.Sample], error) {
	samples, err := listStudySamples(ctx, provider, studyID)
	if err != nil {
		return nil, err
	}

	return prepareDiffStudySamples(store, studyID, samples)
}

func prepareDiffStudySamples(
	store *Store,
	studyID string,
	samples []mlwh.Sample,
) (*PreparedDiff[mlwh.Sample], error) {
	filtered := make([]mlwh.Sample, 0, len(samples))
	for _, sample := range samples {
		filtered = append(filtered, sampleForStudyDiff(sample, studyID))
	}

	return PrepareDiff(store, "study_samples:"+studyID, filtered, func(sample mlwh.Sample) string {
		return sample.Name
	})
}

func sampleForStudyDiff(sample mlwh.Sample, studyID string) mlwh.Sample {
	filtered := sample
	filtered.Studies = filterSampleStudiesForStudy(sample.Studies, studyID)
	filtered.Libraries = filterSampleLibrariesForStudy(sample.Libraries, studyID)

	return filtered
}

func filterSampleStudiesForStudy(studies []mlwh.Study, studyID string) []mlwh.Study {
	if len(studies) == 0 {
		return nil
	}

	filtered := make([]mlwh.Study, 0, len(studies))
	for _, study := range studies {
		if study.IDStudyLims != studyID {
			continue
		}

		filtered = append(filtered, study)
	}
	if len(filtered) == 0 {
		return nil
	}

	return filtered
}

func filterSampleLibrariesForStudy(libraries []mlwh.Library, studyID string) []mlwh.Library {
	if len(libraries) == 0 {
		return nil
	}

	filtered := make([]mlwh.Library, 0, len(libraries))
	for _, library := range libraries {
		if library.IDStudyLims != studyID {
			continue
		}

		filtered = append(filtered, library)
	}
	if len(filtered) == 0 {
		return nil
	}

	return filtered
}

// DiffSampleFiles fetches and diffs the files for one sample.
func DiffSampleFiles(
	ctx context.Context,
	provider Provider,
	store *Store,
	sangerID string,
) (*DiffResult[mlwh.IRODSPath], error) {
	files, err := listSampleFiles(ctx, provider, sangerID)
	if err != nil {
		return nil, err
	}

	var result *DiffResult[mlwh.IRODSPath]

	err = store.WithLock(func() error {
		prepared, err := prepareDiffSampleFiles(store, sangerID, files)
		if err != nil {
			return err
		}

		if err := prepared.Commit(); err != nil {
			return err
		}

		result = prepared.Result

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// PrepareDiffSampleFiles fetches and computes a deferred diff for one sample.
func PrepareDiffSampleFiles(
	ctx context.Context,
	provider Provider,
	store *Store,
	sangerID string,
) (*PreparedDiff[mlwh.IRODSPath], error) {
	files, err := listSampleFiles(ctx, provider, sangerID)
	if err != nil {
		return nil, err
	}

	return PrepareDiffSampleFilesForList(store, sangerID, files)
}

// PrepareDiffSampleFilesForList computes a deferred diff from a pre-fetched file list.
func PrepareDiffSampleFilesForList(
	store *Store,
	sangerID string,
	files []mlwh.IRODSPath,
) (*PreparedDiff[mlwh.IRODSPath], error) {
	return prepareDiffSampleFiles(store, sangerID, files)
}

func prepareDiffSampleFiles(
	store *Store,
	sangerID string,
	files []mlwh.IRODSPath,
) (*PreparedDiff[mlwh.IRODSPath], error) {
	queryKey := "sample_files:" + sangerID

	type sampleFileDiffItem struct {
		Key  string
		File mlwh.IRODSPath
	}

	current := make([]sampleFileDiffItem, 0, len(files))
	for _, file := range files {
		current = append(current, sampleFileDiffItem{Key: sampleFilePreferredKey(file), File: file})
	}

	prepared, err := PrepareDiff(store, queryKey, current, func(item sampleFileDiffItem) string {
		return item.Key
	})
	if err != nil {
		return nil, err
	}

	result := &DiffResult[mlwh.IRODSPath]{
		Added:    make([]mlwh.IRODSPath, 0, len(prepared.Result.Added)),
		Modified: make([]mlwh.IRODSPath, 0, len(prepared.Result.Modified)),
		Removed:  make([]string, len(prepared.Result.Removed)),
	}

	for _, item := range prepared.Result.Added {
		result.Added = append(result.Added, item.File)
	}

	for _, item := range prepared.Result.Modified {
		result.Modified = append(result.Modified, item.File)
	}

	for index, removed := range prepared.Result.Removed {
		result.Removed[index] = sampleFileIdentityValue(removed)
	}

	return &PreparedDiff[mlwh.IRODSPath]{
		Result:          result,
		store:           prepared.store,
		queryKey:        prepared.queryKey,
		previousEntries: prepared.previousEntries,
		nextEntries:     prepared.nextEntries,
	}, nil
}

func sampleFileIdentityValue(key string) string {
	if strings.HasPrefix(key, "id_product:") {
		return strings.TrimPrefix(key, "id_product:")
	}

	return strings.TrimPrefix(key, "irods_path:")
}

func sampleFilePreferredKey(file mlwh.IRODSPath) string {
	if file.IDProduct != "" {
		return "id_product:" + file.IDProduct
	}

	if file.IRODSPath != "" {
		return "irods_path:" + file.IRODSPath
	}

	return "irods_path:" + file.Collection + "/" + file.DataObject
}

func hashItems[T any](items []T) (string, error) {
	serialized := make([]string, 0, len(items))
	for _, item := range items {
		body, err := json.Marshal(item)
		if err != nil {
			return "", fmt.Errorf("seqmeta: marshal hash item: %w", err)
		}

		serialized = append(serialized, string(body))
	}

	sort.Strings(serialized)
	sum := sha256.Sum256([]byte(strings.Join(serialized, "\n")))

	return hex.EncodeToString(sum[:]), nil
}
