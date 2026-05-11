import { type ZodType } from "zod";

export class BackendRequestError extends Error {
    status: number;
    body: unknown;

    constructor(status: number, body: unknown, message?: string) {
        super(message ?? `Backend request failed: ${status}`);
        this.name = "BackendRequestError";
        this.status = status;
        this.body = body;
    }
}

export class BackendUnavailableError extends Error {
    constructor(service: string) {
        super(`${service} backend URL not configured`);
        this.name = "BackendUnavailableError";
    }
}

function getBackendUrl(
    service: string,
    envVar: "WA_RESULTS_BACKEND_URL" | "WA_SEQMETA_BACKEND_URL",
): string {
    const url = process.env[envVar]?.trim();

    if (!url) {
        throw new BackendUnavailableError(service);
    }

    return url;
}

function buildBackendUrl(baseUrl: string, path: string): string {
    const url = new URL(baseUrl);
    const requestUrl = new URL(path, "http://backend.invalid");
    const basePath = url.pathname.replace(/\/$/, "");
    const relativePath = requestUrl.pathname.replace(/^\/+/, "");

    url.pathname = `${basePath}/${relativePath}`;
    url.search = requestUrl.search;
    url.hash = requestUrl.hash;

    return url.toString();
}

async function parseResponseBody(response: Response): Promise<unknown> {
    const contentType = response.headers.get("content-type") ?? "";

    if (contentType.includes("application/json")) {
        return response.json();
    }

    return response.text();
}

async function backendJson<T>(
    service: string,
    envVar: "WA_RESULTS_BACKEND_URL" | "WA_SEQMETA_BACKEND_URL",
    path: string,
    schema: ZodType<T>,
): Promise<T> {
    const baseUrl = getBackendUrl(service, envVar);

    let response: Response;
    try {
        response = await fetch(buildBackendUrl(baseUrl, path));
    } catch {
        throw new BackendRequestError(
            503,
            null,
            `${service} backend request failed`,
        );
    }

    const body = await parseResponseBody(response);

    if (!response.ok) {
        throw new BackendRequestError(response.status, body);
    }

    const parsed = schema.safeParse(body);
    if (!parsed.success) {
        throw new BackendRequestError(
            response.status,
            body,
            `${service} backend response validation failed`,
        );
    }

    return parsed.data;
}

export async function resultsJson<T>(
    path: string,
    schema: ZodType<T>,
): Promise<T> {
    return backendJson("results", "WA_RESULTS_BACKEND_URL", path, schema);
}

export async function seqmetaJson<T>(
    path: string,
    schema: ZodType<T>,
): Promise<T> {
    return backendJson("seqmeta", "WA_SEQMETA_BACKEND_URL", path, schema);
}

export async function resultsRaw(path: string): Promise<Response> {
    const baseUrl = getBackendUrl("results", "WA_RESULTS_BACKEND_URL");

    return fetch(buildBackendUrl(baseUrl, path));
}
