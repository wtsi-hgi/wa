import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import {
    fetchFileContent,
    fetchFiles,
    fetchStats,
    searchResults,
} from "@/app/(results)/actions";
import type { FileEntry, ResultSet } from "@/lib/contracts";

type SearchResultRow = ResultSet | { result_set: ResultSet };

function extractResultSet(row: SearchResultRow): ResultSet {
    return "result_set" in row ? row.result_set : row;
}

function findFile(files: FileEntry[], suffix: string): FileEntry {
    const file = files.find((entry) => entry.path.endsWith(suffix));

    if (!file) {
        throw new Error(`Expected seeded file ending with ${suffix}`);
    }

    return file;
}

async function getSeededAliceResult(): Promise<ResultSet> {
    const rows = await searchResults({ user: ["alice"] });

    if (rows.length === 0) {
        throw new Error("Expected seeded alice result");
    }

    return extractResultSet(rows[0] as SearchResultRow);
}

describe("Q2 API-level integration", () => {
    it("returns seeded stats from the Go server", async () => {
        const stats = await fetchStats();

        expect(stats.total).toBeGreaterThanOrEqual(3);
        expect(stats.recent.length).toBeGreaterThanOrEqual(3);
    });

    it("searches seeded results by requester through the real server action", async () => {
        const rows = await searchResults({ user: ["alice"] });

        expect(rows.length).toBeGreaterThan(0);
        expect(
            rows.map((row) => extractResultSet(row as SearchResultRow).requester),
        ).toEqual(expect.arrayContaining(["alice"]));
        expect(
            rows.every(
                (row) => extractResultSet(row as SearchResultRow).requester === "alice",
            ),
        ).toBe(true);
    });

    it("fetches registered files for a seeded result set", async () => {
        const result = await getSeededAliceResult();

        const files = await fetchFiles(result.id);

        expect(files.length).toBeGreaterThan(0);
        expect(files[0]).toMatchObject({
            path: expect.any(String),
            kind: expect.any(String),
            size: expect.any(Number),
            mtime: expect.any(String),
        });
    });

    it("fetches file content for a seeded text file", async () => {
        const result = await getSeededAliceResult();
        const files = await fetchFiles(result.id);
        const textFile = findFile(files, "report.csv");

        const file = await fetchFileContent(result.id, textFile.path);

        expect(file.content.length).toBeGreaterThan(0);
        expect(file.contentType).toMatch(/^[a-z]+\/[a-z0-9.+-]+/i);
    });

    it("streams image content through the file API route", async () => {
        const result = await getSeededAliceResult();
        const files = await fetchFiles(result.id);
        const imageFile = findFile(files, "image.png");
        const request = new NextRequest(
            `http://localhost/api/file?id=${encodeURIComponent(result.id)}&path=${encodeURIComponent(imageFile.path)}`,
        );
        const { GET } = await import("@/app/api/file/route");

        const response = await GET(request);

        expect(response.status).toBe(200);
        expect(response.headers.get("content-type")).toMatch(/^image\//);
    });

    it("returns 403 for unregistered file paths through the file API route", async () => {
        const result = await getSeededAliceResult();
        const request = new NextRequest(
            `http://localhost/api/file?id=${encodeURIComponent(result.id)}&path=${encodeURIComponent("/tmp/not-registered.txt")}`,
        );
        const { GET } = await import("@/app/api/file/route");

        const response = await GET(request);

        expect(response.status).toBe(403);
    });

    it("returns no rows for unmatched requester searches", async () => {
        await expect(searchResults({ user: ["nonexistent"] })).resolves.toEqual([]);
    });
});
