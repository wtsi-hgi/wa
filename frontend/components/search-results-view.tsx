"use client";

import { useState } from "react";

import {
    ResultsTable,
    type ResultsTableProps,
} from "@/components/results-table";
import {
    SearchCombinedFileBrowser,
    type CombinedSearchFile,
    type CombinedSearchRegistration,
    type SearchFileMode,
} from "@/components/search-combined-file-browser";

type SearchResultsViewProps = {
    combinedFiles: CombinedSearchFile[];
    lockedRegistrations: CombinedSearchRegistration[];
    registrations: CombinedSearchRegistration[];
    resultsTable: ResultsTableProps;
};

export function SearchResultsView({
    combinedFiles,
    lockedRegistrations,
    registrations,
    resultsTable,
}: SearchResultsViewProps) {
    const [mode, setMode] = useState<SearchFileMode>("combined");
    const { hideSummary: _hideSummary, ...resultRowsTable } = resultsTable;

    return (
        <>
            <SearchCombinedFileBrowser
                files={combinedFiles}
                lockedRegistrations={lockedRegistrations}
                mode={mode}
                onModeChange={setMode}
                registrations={registrations}
            />
            {mode === "rows" ? (
                <ResultsTable {...resultRowsTable} hideSummary={false} />
            ) : null}
        </>
    );
}
