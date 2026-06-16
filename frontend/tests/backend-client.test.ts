import {
    mkdir,
    mkdtemp,
    readdir,
    readFile,
    rm,
    writeFile,
} from "node:fs/promises";
import { createServer } from "node:https";
import type { AddressInfo } from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";

import {
    BackendRequestError,
    BackendUnavailableError,
    mlwhJson,
    resultsJson,
    resultsRaw,
} from "@/lib/backend-client";

const agentTmpRoot = fileURLToPath(
    new URL("../../.tmp/agent/", import.meta.url),
);
const frontendRoot = fileURLToPath(new URL("../", import.meta.url));

const ignoredSourceDirectories = new Set([
    ".next",
    ".turbo",
    "coverage",
    "node_modules",
]);
const searchableExtensions = new Set([
    ".cjs",
    ".js",
    ".json",
    ".mjs",
    ".ts",
    ".tsx",
]);

async function collectFrontendTextFiles(directory: string): Promise<string[]> {
    const entries = await readdir(directory, { withFileTypes: true });
    const files: string[] = [];

    for (const entry of entries) {
        if (ignoredSourceDirectories.has(entry.name)) {
            continue;
        }

        const entryPath = path.join(directory, entry.name);

        if (entry.isDirectory()) {
            files.push(...(await collectFrontendTextFiles(entryPath)));
            continue;
        }

        if (searchableExtensions.has(path.extname(entry.name))) {
            files.push(entryPath);
        }
    }

    return files;
}

const testServerKey = `-----BEGIN PRIVATE KEY-----
MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQC/m6zwd86IsLNZ
pKUQHeBwxEwaHLaeZYPmk0Cg615XmgT0UVZ7oNLlQVhyaxtdjuvWzOOBBYrhe23P
4lNmrDALlwNDuhWWtBxhUK2LVA6hmiFfkNWtGhOBf8y9n/jU6UmEg2FUJidkFnlZ
xwiLaU/ZCbyxLluRN0f5BPCI/bSez5yKkxAn8dFgTUH90b2VxVyEcvRKt0QL8YYl
HcOnHl3Y3vH5lGGZr38zOKNWtTu0Z79dJHchPJazD8I9PU5rGc7A79YY56ftNlld
j7Mne6bDgpxlD98oeyUJ+cWYK1P5O/6VtYryLSXRgapDjuBGTV/AeKCWVHuGMPN0
xogsz/HRAgMBAAECggEAESGWk0Nq9F60EmI9ndTGAd8THMyPaVcTNXTZ9OlGXJe5
NKznChOld3jhsw0ve6xxGpnkB1+a/LD/7vPB2C6x9v9P++ix0HEXDn5bndbsnfc9
X6F/8UOhFdV61UNtyH95Ir9qXs8we2rk+6lncqt+R53uwHqwFio2paWS6fShBwfK
LMDtzmknmDdwehP2nXc2mlnmM14EtMF7ur+VOL7+Ag5h2IDhX5+tCKTOB6xGUWdb
hikWgIbXQbWbdYvk4Amg/CBw9hUn4TXi7XGVdaW0A48ZR9lOm66xr6g/ZXUWKPWS
RRgCnTim9rqlsusYJHlIj4jEt+2945wXOoZY0Fv09wKBgQDxDYSMoTYPmFlJtU0T
WQSTYjAC8SsQLi8TBP7/2Zdr67Nb8yekym5IzZVsoN0e4sKPNdjRNKu45FegzMsE
qYUveEPvxXxSKv3HURiffh2BFcmEBiKXVP6s/2aVs5wAZZijFRtXeC6/6fdfJTIs
dYJCGIVNisYot/EL6lvhtCx1CwKBgQDLfUVGt5ojMWWOAdpJ1snp8Sd1rKLPD+Kg
ujjZU9eFBxVDzlbPh8xKuHr3xreK0mb0ib7UOu4nSpuBqL9AQbNVyVziCxBf5omQ
s6S3B318zmt8xajSFS6vYX+iMrMgBVSeVyaAZ2+Rd+LgpNOJK+PG9jTN5F41CZ/2
oWTj6W8GEwKBgHmcDz3/as2tV4ZnEA5tv3A3fe9OCiKsmhUnVRpwhQLuM1t1LY/m
jILwLK1T7ppRXkRvrwXEY8nwcQDvsJCWkVmke+mwIQs3Izb2A80bC/l+q16O1c6x
E5bldrSZm19b4giMcnHLcRJjD+iRVGG3mtKLmlzHYTdTrSkMv/P7ON6HAoGAMgiB
KhwmyBRzNfF6rMElMGJdI2/pMCRlwsNHCxi0Wz8cmWl4qtpm/tBRW+7+XiHRsrrT
svcya3LKvZyyOaht4d/6+JFj21Ch7nRdQauTzUYr46fuFImkyvacHVN9+5eT8MLY
8qV8JzZlEHs2j/m8rcUHwsAt8biGHmwclHVnGQUCgYBT+HBNcdc8VPzoGMCbiTeC
LvWIqSaJJp0NCkUi6Q7GEDINRwdZYY29G5be3RzDkd3HENJRdSARjqgQTWlvcuKw
HZd2PBpbwWTaNdwQzDIkzUZV/K9376U0b6Th8M/xwEBzsrscTfQ5HOo+Y8NGySvk
D1633aEzQaQR3QIdx4v3nQ==
-----END PRIVATE KEY-----
`;

const testServerCert = `-----BEGIN CERTIFICATE-----
MIIDJTCCAg2gAwIBAgIUESM00vqXqMiWCmA9JyR4PkaT6SUwDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJMTI3LjAuMC4xMB4XDTI2MDUyMTEyMjAwOVoXDTM2MDUx
ODEyMjAwOVowFDESMBAGA1UEAwwJMTI3LjAuMC4xMIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAv5us8HfOiLCzWaSlEB3gcMRMGhy2nmWD5pNAoOteV5oE
9FFWe6DS5UFYcmsbXY7r1szjgQWK4Xttz+JTZqwwC5cDQ7oVlrQcYVCti1QOoZoh
X5DVrRoTgX/MvZ/41OlJhINhVCYnZBZ5WccIi2lP2Qm8sS5bkTdH+QTwiP20ns+c
ipMQJ/HRYE1B/dG9lcVchHL0SrdEC/GGJR3Dpx5d2N7x+ZRhma9/MzijVrU7tGe/
XSR3ITyWsw/CPT1OaxnOwO/WGOen7TZZXY+zJ3umw4KcZQ/fKHslCfnFmCtT+Tv+
lbWK8i0l0YGqQ47gRk1fwHigllR7hjDzdMaILM/x0QIDAQABo28wbTAdBgNVHQ4E
FgQU9l9kFZ2XlJb1t805a5uUARv/ng4wHwYDVR0jBBgwFoAU9l9kFZ2XlJb1t805
a5uUARv/ng4wDwYDVR0TAQH/BAUwAwEB/zAaBgNVHREEEzARhwR/AAABgglsb2Nh
bGhvc3QwDQYJKoZIhvcNAQELBQADggEBAI+ucCitl+JiVVuwyj8AsOK+SHzx6bQg
ICIgRM8flXcspM7D7tDaODwQFhBmWNGsyyEOgDpMAw9pDLKZDjysTBYFkQ2/5bUM
3msQBVD6BqJbkwJRbGmtWJasfdqHaQUisXr0VRFx/Qx6zfETX9f0KXJ0j2zVleJk
M9qfCjNWkySVeFi/4bAdYKXpV1bT5iW8eOytgrETHcbMefOYrn+dY1jOHyHZP9km
kE5xeU08LD2/pL8q0tovGzBWB3pY4knSbdaNBFo1Lhlq+hQ3DB3pjqP5fFIgprcL
rg6h9EFLeqNkHiOGaTyxe5qMhdW4GO4mKnYX/w0pILvqiH25Ew/Nb74=
-----END CERTIFICATE-----
`;

describe("H1 dual backend client", () => {
    afterEach(() => {
        delete process.env.WA_RESULTS_BACKEND_CA_CERT;
        delete process.env.WA_RESULTS_BACKEND_URL;
        delete process.env.WA_MLWH_BACKEND_URL;
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it("rejects non-HTTPS results backend URLs", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "http://localhost:8090";

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).rejects.toThrow("results backend URL must use https");
    });

    it("returns validated results JSON from the configured results backend", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://localhost:8090";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(Response.json({ total: 5 }, { status: 200 }));
        vi.stubGlobal("fetch", fetchMock);

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).resolves.toEqual({
            total: 5,
        });
        expect(fetchMock).toHaveBeenCalledWith(
            "https://localhost:8090/results/stats",
        );
    });

    it("preserves a path prefix in the configured results backend URL", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://host/results-api";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(Response.json({ total: 5 }, { status: 200 }));
        vi.stubGlobal("fetch", fetchMock);

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).resolves.toEqual({
            total: 5,
        });
        expect(fetchMock).toHaveBeenCalledWith(
            "https://host/results-api/results/stats",
        );
    });

    it("throws BackendRequestError with the response status for non-2xx responses", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://localhost:8090";

        vi.stubGlobal(
            "fetch",
            vi
                .fn()
                .mockResolvedValue(
                    Response.json({ error: "not found" }, { status: 404 }),
                ),
        );

        const statsSchema = z.object({ total: z.number() });

        const result = resultsJson("/results/stats", statsSchema);

        await expect(result).rejects.toMatchObject({
            status: 404,
            body: { error: "not found" },
        });
        await expect(result).rejects.toBeInstanceOf(BackendRequestError);
    });

    it("throws BackendRequestError when the response body fails schema validation", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://localhost:8090";

        vi.stubGlobal(
            "fetch",
            vi
                .fn()
                .mockResolvedValue(
                    Response.json({ bad: "shape" }, { status: 200 }),
                ),
        );

        const statsSchema = z.object({ total: z.number() });

        await expect(
            resultsJson("/results/stats", statsSchema),
        ).rejects.toBeInstanceOf(BackendRequestError);
    });

    it("uses the configured results CA certificate only for results backend requests", async () => {
        await mkdir(agentTmpRoot, { recursive: true });
        const tempDir = await mkdtemp(
            path.join(agentTmpRoot, "backend-client-"),
        );
        const seenResultsPaths: string[] = [];
        const server = createServer(
            { cert: testServerCert, key: testServerKey },
            (request, response) => {
                seenResultsPaths.push(request.url ?? "");
                response.setHeader("content-type", "application/json");
                response.end(JSON.stringify({ total: 5 }));
            },
        );

        await new Promise<void>((resolve) => {
            server.listen(0, "127.0.0.1", () => resolve());
        });

        const address = server.address() as AddressInfo;
        process.env.WA_RESULTS_BACKEND_URL = `https://127.0.0.1:${address.port}`;
        process.env.WA_MLWH_BACKEND_URL = "https://mlwh.example";
        const caPath = path.join(tempDir, "wa-results-ca.pem");
        await writeFile(caPath, testServerCert);
        process.env.WA_RESULTS_BACKEND_CA_CERT = caPath;
        const fetchMock = vi
            .fn()
            .mockResolvedValue(
                Response.json([{ id: "study-1" }], { status: 200 }),
            );
        vi.stubGlobal("fetch", fetchMock);

        try {
            await expect(
                resultsJson("/results/stats", z.object({ total: z.number() })),
            ).resolves.toEqual({ total: 5 });
            await expect(
                mlwhJson("/studies", z.array(z.object({ id: z.string() }))),
            ).resolves.toEqual([{ id: "study-1" }]);
        } finally {
            await new Promise<void>((resolve) => {
                server.close(() => resolve());
            });
            await rm(tempDir, { force: true, recursive: true });
        }

        expect(seenResultsPaths).toEqual(["/results/stats"]);
        expect(fetchMock).toHaveBeenCalledTimes(1);
        expect(fetchMock).toHaveBeenCalledWith("https://mlwh.example/studies");
    });

    it("throws BackendUnavailableError when the MLWH backend URL is not configured", async () => {
        const studiesSchema = z.array(z.object({ id: z.string() }));

        await expect(
            mlwhJson("/studies", studiesSchema),
        ).rejects.toBeInstanceOf(BackendUnavailableError);
    });

    it("returns validated MLWH JSON from the configured MLWH backend", async () => {
        process.env.WA_MLWH_BACKEND_URL = "http://localhost:8091";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(
                Response.json([{ id: "study-1" }], { status: 200 }),
            );
        vi.stubGlobal("fetch", fetchMock);

        const studiesSchema = z.array(z.object({ id: z.string() }));

        await expect(mlwhJson("/studies", studiesSchema)).resolves.toEqual([
            { id: "study-1" },
        ]);
        expect(fetchMock).toHaveBeenCalledWith("http://localhost:8091/studies");
    });

    it("preserves a path prefix in the configured MLWH backend URL", async () => {
        process.env.WA_MLWH_BACKEND_URL = "https://host/mlwh-api";

        const fetchMock = vi
            .fn()
            .mockResolvedValue(
                Response.json([{ id: "study-1" }], { status: 200 }),
            );
        vi.stubGlobal("fetch", fetchMock);

        const studiesSchema = z.array(z.object({ id: z.string() }));

        await expect(mlwhJson("/studies", studiesSchema)).resolves.toEqual([
            { id: "study-1" },
        ]);
        expect(fetchMock).toHaveBeenCalledWith("https://host/mlwh-api/studies");
    });

    it("throws BackendRequestError with parsed MLWH error envelopes", async () => {
        process.env.WA_MLWH_BACKEND_URL = "http://localhost:8091";

        vi.stubGlobal(
            "fetch",
            vi.fn().mockResolvedValue(
                Response.json(
                    {
                        code: "not_found",
                        message: "identifier not found",
                    },
                    { status: 404 },
                ),
            ),
        );

        const studiesSchema = z.array(z.object({ id: z.string() }));
        const result = mlwhJson("/studies", studiesSchema);

        await expect(result).rejects.toMatchObject({
            status: 404,
            body: {
                code: "not_found",
                message: "identifier not found",
            },
        });
        await expect(result).rejects.toBeInstanceOf(BackendRequestError);
    });

    it("does not reference retired frontend backend client names", async () => {
        const files = await collectFrontendTextFiles(frontendRoot);
        const sourceText = (
            await Promise.all(files.map((file) => readFile(file, "utf8")))
        ).join("\n");

        expect(sourceText).not.toContain("WA_" + "SEQMETA_BACKEND_URL");
        expect(sourceText).not.toContain("seqmeta" + "Json");
    });

    it("returns the raw response for results file requests", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://localhost:8090";

        const binaryBody = new Uint8Array([137, 80, 78, 71]);
        const response = new Response(binaryBody, {
            status: 200,
            headers: { "content-type": "image/png" },
        });

        const fetchMock = vi.fn().mockResolvedValue(response);
        vi.stubGlobal("fetch", fetchMock);

        const rawResponse = await resultsRaw("/results/1/file?path=/tmp/x.png");

        expect(fetchMock).toHaveBeenCalledWith(
            "https://localhost:8090/results/1/file?path=/tmp/x.png",
        );
        expect(rawResponse.status).toBe(200);
        expect(rawResponse.headers.get("content-type")).toBe("image/png");
        expect(new Uint8Array(await rawResponse.arrayBuffer())).toEqual(
            binaryBody,
        );
    });

    it("preserves a path prefix and query string for raw results file requests", async () => {
        process.env.WA_RESULTS_BACKEND_URL = "https://host/results-api";

        const response = new Response(new Uint8Array([137, 80, 78, 71]), {
            status: 200,
            headers: { "content-type": "image/png" },
        });
        const fetchMock = vi.fn().mockResolvedValue(response);
        vi.stubGlobal("fetch", fetchMock);

        await resultsRaw("/results/1/file?path=/tmp/x.png");

        expect(fetchMock).toHaveBeenCalledWith(
            "https://host/results-api/results/1/file?path=/tmp/x.png",
        );
    });
});
