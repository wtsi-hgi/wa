import { useMemo, useSyncExternalStore } from "react";

import type { FileEntry } from "@/lib/contracts";

export const fileBrowserGlobFilterStoragePrefix =
    "wa:file-browser:glob-filter:";

const regexSpecialCharacters = /[\\^$+?.()|{}\[\]]/g;

function escapeRegexCharacter(character: string): string {
    return character.replace(regexSpecialCharacters, "\\$&");
}

function normalizePath(path: string): string {
    return path.replaceAll("\\", "/").replaceAll(/\/+/g, "/");
}

function fileName(path: string): string {
    return normalizePath(path).split("/").filter(Boolean).at(-1) ?? path;
}

function pathCandidates(path: string): string[] {
    const normalizedPath = normalizePath(path);
    const withoutLeadingSlash = normalizedPath.replace(/^\/+/, "");

    return [
        normalizedPath,
        withoutLeadingSlash,
        fileName(normalizedPath),
    ].filter((candidate, index, candidates) => {
        return candidate.length > 0 && candidates.indexOf(candidate) === index;
    });
}

function classBodyFromGlobClass(globClass: string): string {
    const negate = globClass.startsWith("!") || globClass.startsWith("^");
    const body = negate ? globClass.slice(1) : globClass;
    const escapedBody = body
        .split("")
        .map((character, index) => {
            if (character === "\\") {
                return "\\\\";
            }

            if (character === "]") {
                return "\\]";
            }

            if (character === "^" && index === 0) {
                return "\\^";
            }

            return character;
        })
        .join("");

    return `[${negate ? "^" : ""}${escapedBody}]`;
}

function globPatternToRegexSource(
    pattern: string,
    options: { starMatchesSlash?: boolean } = {},
): string {
    let source = "";

    for (let index = 0; index < pattern.length; index += 1) {
        const character = pattern[index];

        if (character === "\\") {
            const nextCharacter = pattern[index + 1];

            if (nextCharacter) {
                source += escapeRegexCharacter(nextCharacter);
                index += 1;
            } else {
                source += "\\\\";
            }

            continue;
        }

        if (character === "*") {
            const nextCharacter = pattern[index + 1];

            if (nextCharacter === "*") {
                while (pattern[index + 1] === "*") {
                    index += 1;
                }

                if (pattern[index + 1] === "/") {
                    source += "(?:[^/]+/)*";
                    index += 1;
                } else {
                    source += ".*";
                }

                continue;
            }

            source += options.starMatchesSlash ? ".*" : "[^/]*";
            continue;
        }

        if (character === "?") {
            source += "[^/]";
            continue;
        }

        if (character === "[") {
            const closeIndex = pattern.indexOf("]", index + 1);

            if (closeIndex > index + 1) {
                source += classBodyFromGlobClass(
                    pattern.slice(index + 1, closeIndex),
                );
                index = closeIndex;
                continue;
            }
        }

        source += escapeRegexCharacter(character ?? "");
    }

    return source;
}

function shouldAddPathWideGlob(pattern: string): boolean {
    const normalizedPattern = normalizePath(pattern);

    return (
        !normalizedPattern.startsWith("/") &&
        !normalizedPattern.includes("/") &&
        normalizedPattern.includes("*")
    );
}

function compileGlobPattern(pattern: string): RegExp[] {
    const trimmedPattern = pattern.trim();

    if (!trimmedPattern) {
        return [];
    }

    const patterns: Array<{
        options?: { starMatchesSlash?: boolean };
        pattern: string;
    }> = [{ pattern: trimmedPattern }];

    if (!trimmedPattern.startsWith("/") && !trimmedPattern.startsWith("**/")) {
        patterns.push({ pattern: `**/${trimmedPattern}` });
    }

    if (shouldAddPathWideGlob(trimmedPattern)) {
        patterns.push({
            options: { starMatchesSlash: true },
            pattern: trimmedPattern,
        });
    }

    return patterns
        .map(({ options, pattern: candidatePattern }) => {
            try {
                return new RegExp(
                    `^${globPatternToRegexSource(
                        normalizePath(candidatePattern),
                        options ?? {},
                    )}$`,
                );
            } catch {
                return null;
            }
        })
        .filter((regex): regex is RegExp => regex !== null);
}

function filePathMatchesCompiledGlobPattern(
    path: string,
    regexes: readonly RegExp[],
): boolean {
    if (regexes.length === 0) {
        return true;
    }

    const candidates = pathCandidates(path);

    return regexes.some((regex) =>
        candidates.some((candidate) => regex.test(candidate)),
    );
}

export function fileBrowserGlobFilterStorageKey(
    storageScope: string | undefined,
): string | undefined {
    const trimmedScope = storageScope?.trim();

    if (!trimmedScope) {
        return undefined;
    }

    return `${fileBrowserGlobFilterStoragePrefix}${trimmedScope}`;
}

export function readSavedFileBrowserGlobFilter(
    storageScope: string | undefined,
): string {
    const storageKey = fileBrowserGlobFilterStorageKey(storageScope);

    return readSavedFileBrowserGlobFilterByKey(storageKey);
}

function readSavedFileBrowserGlobFilterByKey(
    storageKey: string | undefined,
): string {
    if (!storageKey || typeof window === "undefined") {
        return "";
    }

    try {
        return window.localStorage.getItem(storageKey) ?? "";
    } catch {
        return "";
    }
}

function emptyFileBrowserGlobFilterSnapshot(): string {
    return "";
}

function subscribeToFileBrowserGlobFilterStorage(): () => void {
    return () => undefined;
}

export function useSavedFileBrowserGlobFilter(
    storageScope: string | undefined,
): string {
    const storageKey = useMemo(
        () => fileBrowserGlobFilterStorageKey(storageScope),
        [storageScope],
    );

    return useSyncExternalStore(
        subscribeToFileBrowserGlobFilterStorage,
        () => readSavedFileBrowserGlobFilterByKey(storageKey),
        emptyFileBrowserGlobFilterSnapshot,
    );
}

export function saveFileBrowserGlobFilter(
    storageScope: string | undefined,
    value: string,
): void {
    const storageKey = fileBrowserGlobFilterStorageKey(storageScope);

    if (!storageKey || typeof window === "undefined") {
        return;
    }

    try {
        if (value.trim()) {
            window.localStorage.setItem(storageKey, value);
        } else {
            window.localStorage.removeItem(storageKey);
        }
    } catch {
        // Browser storage can be unavailable; filtering still works in memory.
    }
}

export function filePathMatchesGlobPattern(
    path: string,
    pattern: string,
): boolean {
    const regexes = compileGlobPattern(pattern);

    return filePathMatchesCompiledGlobPattern(path, regexes);
}

export function filterFilesByGlobPattern(
    files: FileEntry[],
    pattern: string,
): FileEntry[] {
    const regexes = compileGlobPattern(pattern);

    if (regexes.length === 0) {
        return files;
    }

    return files.filter((file) =>
        filePathMatchesCompiledGlobPattern(file.path, regexes),
    );
}
