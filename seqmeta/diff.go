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
	"strconv"
	"strings"
	"time"

	"github.com/wtsi-hgi/wa/saga"
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
	provider SAGAProvider,
	store *Store,
	studyID string,
) (*DiffResult[saga.MLWHSample], error) {
	samples, err := provider.AllSamplesForStudy(ctx, studyID)
	if err != nil {
		return nil, err
	}

	var result *DiffResult[saga.MLWHSample]

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
	provider SAGAProvider,
	store *Store,
	studyID string,
) (*PreparedDiff[saga.MLWHSample], error) {
	samples, err := provider.AllSamplesForStudy(ctx, studyID)
	if err != nil {
		return nil, err
	}

	return prepareDiffStudySamples(store, studyID, samples)
}

func prepareDiffStudySamples(
	store *Store,
	studyID string,
	samples []saga.MLWHSample,
) (*PreparedDiff[saga.MLWHSample], error) {
	return PrepareDiff(store, "study_samples:"+studyID, samples, func(sample saga.MLWHSample) string {
		return sample.SangerID
	})
}

// DiffSampleFiles fetches and diffs the files for one sample.
func DiffSampleFiles(
	ctx context.Context,
	provider SAGAProvider,
	store *Store,
	sangerID string,
) (*DiffResult[saga.IRODSFile], error) {
	files, err := provider.GetSampleFiles(ctx, sangerID)
	if err != nil {
		return nil, err
	}

	var result *DiffResult[saga.IRODSFile]

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
	provider SAGAProvider,
	store *Store,
	sangerID string,
) (*PreparedDiff[saga.IRODSFile], error) {
	files, err := provider.GetSampleFiles(ctx, sangerID)
	if err != nil {
		return nil, err
	}

	return prepareDiffSampleFiles(store, sangerID, files)
}

func prepareDiffSampleFiles(
	store *Store,
	sangerID string,
	files []saga.IRODSFile,
) (*PreparedDiff[saga.IRODSFile], error) {
	queryKey := "sample_files:" + sangerID
	previous, err := store.LoadEntries(queryKey)
	if err != nil {
		return nil, err
	}

	type sampleFileDiffItem struct {
		Key  string
		File saga.IRODSFile
	}

	collectionOnlyKeys := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.ID == 0 {
			collectionOnlyKeys[sampleFileCollectionKey(file.Collection)] = struct{}{}
		}
	}

	current := make([]sampleFileDiffItem, 0, len(files))
	for _, file := range files {
		key := sampleFilePreferredKey(file)
		if file.ID != 0 {
			collectionKey := sampleFileCollectionKey(file.Collection)
			_, collectionClaimed := collectionOnlyKeys[collectionKey]
			_, previousCollection := previous[collectionKey]
			_, previousID := previous[sampleFileIDKey(file.ID)]
			if previousCollection && !collectionClaimed && !previousID {
				key = collectionKey
			}
		}

		current = append(current, sampleFileDiffItem{Key: key, File: file})
	}

	prepared, err := PrepareDiff(store, queryKey, current, func(item sampleFileDiffItem) string {
		return item.Key
	})
	if err != nil {
		return nil, err
	}

	result := &DiffResult[saga.IRODSFile]{
		Added:    make([]saga.IRODSFile, 0, len(prepared.Result.Added)),
		Modified: make([]saga.IRODSFile, 0, len(prepared.Result.Modified)),
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

	return &PreparedDiff[saga.IRODSFile]{
		Result:          result,
		store:           prepared.store,
		queryKey:        prepared.queryKey,
		previousEntries: prepared.previousEntries,
		nextEntries:     prepared.nextEntries,
	}, nil
}

func sampleFileIdentityValue(key string) string {
	if strings.HasPrefix(key, "id:") {
		return strings.TrimPrefix(key, "id:")
	}

	return strings.TrimPrefix(key, "collection:")
}

func sampleFilePreferredKey(file saga.IRODSFile) string {
	if file.ID != 0 {
		return sampleFileIDKey(file.ID)
	}

	return sampleFileCollectionKey(file.Collection)
}

func sampleFileIDKey(id int) string {
	return "id:" + strconv.Itoa(id)
}

func sampleFileCollectionKey(collection string) string {
	return "collection:" + collection
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
