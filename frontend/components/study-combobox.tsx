"use client";

import { useMemo, useState } from "react";

import type { Study } from "@/lib/contracts";

type StudyComboboxProps = {
    studies: Study[];
    onSelect: (studyId: string) => void;
};

export function StudyCombobox({ onSelect, studies = [] }: StudyComboboxProps) {
    const [query, setQuery] = useState("");

    const normalizedQuery = query.trim().toLowerCase();
    const filteredStudies = useMemo(
        () =>
            studies.filter(
                (study) =>
                    normalizedQuery.length === 0 ||
                    study.name.toLowerCase().includes(normalizedQuery) ||
                    study.id_study_lims.toLowerCase().includes(normalizedQuery),
            ),
        [normalizedQuery, studies],
    );

    if (studies.length === 0) {
        return (
            <div className="space-y-2">
                <label
                    htmlFor="study-search"
                    className="text-sm font-medium text-foreground"
                >
                    Study
                </label>
                <input
                    id="study-search"
                    disabled
                    placeholder="No studies available"
                    className="h-11 w-full rounded-xl border border-border bg-muted/40 px-3 text-sm text-muted-foreground outline-none"
                />
            </div>
        );
    }

    return (
        <div className="space-y-3">
            <div className="space-y-2">
                <label
                    htmlFor="study-search"
                    className="text-sm font-medium text-foreground"
                >
                    Study
                </label>
                <input
                    id="study-search"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder="Search studies"
                    className="h-11 w-full rounded-xl border border-border bg-background px-3 text-sm outline-none transition focus:border-primary focus:ring-2 focus:ring-ring/30"
                />
            </div>

            <div className="max-h-56 space-y-2 overflow-y-auto rounded-xl border border-border/70 bg-muted/20 p-2">
                {filteredStudies.length > 0 ? (
                    filteredStudies.map((study) => (
                        <button
                            key={study.id_study_lims}
                            type="button"
                            onClick={() => onSelect(study.id_study_lims)}
                            className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left text-sm transition hover:bg-accent/45"
                        >
                            <span className="font-medium text-foreground">{study.name}</span>
                            <span className="text-xs text-muted-foreground">
                                {study.id_study_lims}
                            </span>
                        </button>
                    ))
                ) : (
                    <p className="px-2 py-3 text-sm text-muted-foreground">
                        No studies match.
                    </p>
                )}
            </div>
        </div>
    );
}
