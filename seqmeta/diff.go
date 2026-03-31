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
	"sort"
	"strings"
	"time"

	"github.com/wtsi-hgi/wa/saga"
)

// Diff computes the change set for one logical query.
func Diff[T any](
	store *Store,
	queryKey string,
	current []T,
	idFunc func(T) string,
) (*DiffResult[T], error) {
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
	for _, item := range current {
		entryID := idFunc(item)
		group := currentGroups[entryID]
		group.items = append(group.items, item)
		currentGroups[entryID] = group
	}

	for entryID, group := range currentGroups {
		group.hash = hashItems(group.items)
		currentGroups[entryID] = group
	}

	now := time.Now().UTC()
	nextEntries := make(map[string]StoredEntry, len(previous)+len(currentGroups))
	for entryID, entry := range previous {
		nextEntries[entryID] = entry
	}

	for entryID, group := range currentGroups {
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

	if err := store.SaveEntries(queryKey, nextEntries); err != nil {
		return nil, err
	}

	return result, nil
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

	return Diff(store, "study_samples:"+studyID, samples, func(sample saga.MLWHSample) string {
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

	return Diff(store, "sample_files:"+sangerID, files, func(file saga.IRODSFile) string {
		return file.Collection
	})
}

func hashItems[T any](items []T) string {
	serialized := make([]string, 0, len(items))
	for _, item := range items {
		body, err := json.Marshal(item)
		if err != nil {
			panic(err)
		}

		serialized = append(serialized, string(body))
	}

	sort.Strings(serialized)
	sum := sha256.Sum256([]byte(strings.Join(serialized, "\n")))

	return hex.EncodeToString(sum[:])
}
