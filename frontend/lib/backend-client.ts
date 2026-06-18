import * as fs from "node:fs";
import * as https from "node:https";
import { Readable } from "node:stream";

import { type ZodType } from "zod";

import {
    BackendRequestError,
    BackendUnavailableError,
} from "@/lib/backend-shared";

export {
    BackendRequestError,
    BackendUnavailableError,
    resultsAuthCookieName,
} from "@/lib/backend-shared";

type BackendEnvVar = "WA_RESULTS_BACKEND_URL" | "WA_MLWH_BACKEND_URL";
type BackendFetchOptions = Omit<RequestInit, "headers"> & {
    headers?: HeadersInit;
    jwt?: string | null;
};
type FetchInitWithAgent = RequestInit & {
    agent?: https.Agent;
};

function getBackendUrl(service: string, envVar: BackendEnvVar): string {
    const url = process.env[envVar]?.trim();

    if (!url) {
        throw new BackendUnavailableError(service);
    }

    if (envVar === "WA_RESULTS_BACKEND_URL") {
        const parsed = new URL(url);

        if (parsed.protocol !== "https:") {
            throw new Error("results backend URL must use https");
        }
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

export function resultsBackendUrl(path: string): string {
    return buildBackendUrl(
        getBackendUrl("results", "WA_RESULTS_BACKEND_URL"),
        path,
    );
}

let resultsAgentCache: {
    caCertPath: string;
    agent: https.Agent;
} | null = null;

function resultsHttpsAgent(): https.Agent | undefined {
    const caCertPath = process.env.WA_RESULTS_BACKEND_CA_CERT?.trim();

    if (!caCertPath) {
        return undefined;
    }

    if (resultsAgentCache?.caCertPath === caCertPath) {
        return resultsAgentCache.agent;
    }

    const ca = fs.readFileSync(caCertPath, "utf8");
    const agent = new https.Agent({ ca });
    resultsAgentCache = { caCertPath, agent };

    return agent;
}

function buildHeaders(
    headers: HeadersInit | undefined,
    jwt: string | null | undefined,
): Record<string, string> | undefined {
    const merged = new Headers(headers);

    if (jwt) {
        merged.set("authorization", `Bearer ${jwt}`);
    }

    const entries = Object.fromEntries(merged.entries());

    return Object.keys(entries).length > 0 ? entries : undefined;
}

function buildFetchInit(
    options: BackendFetchOptions | undefined,
    agent: https.Agent | undefined,
): FetchInitWithAgent | undefined {
    const { headers, jwt, ...rest } = options ?? {};
    const init: FetchInitWithAgent = { ...rest };
    const mergedHeaders = buildHeaders(headers, jwt);

    if (mergedHeaders) {
        init.headers = mergedHeaders;
    }

    if (agent) {
        init.agent = agent;
    }

    return Object.keys(init).length > 0 ? init : undefined;
}

function fetchBackend(
    url: string,
    init: FetchInitWithAgent | undefined,
): Promise<Response> {
    if (init?.agent) {
        return fetchWithHttpsAgent(url, init, init.agent);
    }

    return init ? fetch(url, init) : fetch(url);
}

function responseHeaders(headers: NodeJS.Dict<string | string[]>): Headers {
    const responseHeaders = new Headers();

    for (const [name, value] of Object.entries(headers)) {
        if (Array.isArray(value)) {
            for (const item of value) {
                responseHeaders.append(name, item);
            }
        } else if (typeof value === "string") {
            responseHeaders.set(name, value);
        }
    }

    return responseHeaders;
}

async function requestBodyBuffer(
    body: BodyInit | null | undefined,
): Promise<Buffer | undefined> {
    if (body === null || body === undefined) {
        return undefined;
    }

    if (typeof body === "string") {
        return Buffer.from(body);
    }

    if (body instanceof URLSearchParams) {
        return Buffer.from(body.toString());
    }

    if (body instanceof ArrayBuffer) {
        return Buffer.from(body);
    }

    if (ArrayBuffer.isView(body)) {
        return Buffer.from(body.buffer, body.byteOffset, body.byteLength);
    }

    if (body instanceof Blob) {
        return Buffer.from(await body.arrayBuffer());
    }

    throw new Error("unsupported results backend request body");
}

async function fetchWithHttpsAgent(
    url: string,
    init: FetchInitWithAgent,
    agent: https.Agent,
): Promise<Response> {
    const parsedUrl = new URL(url);
    const headers = new Headers(init.headers);
    const body = await requestBodyBuffer(init.body);

    if (body && !headers.has("content-length")) {
        headers.set("content-length", String(body.byteLength));
    }

    return new Promise<Response>((resolve, reject) => {
        const request = https.request(
            parsedUrl,
            {
                agent,
                headers: Object.fromEntries(headers.entries()),
                method: init.method ?? "GET",
                signal: init.signal ?? undefined,
            },
            (response) => {
                resolve(
                    new Response(
                        Readable.toWeb(response) as ReadableStream<Uint8Array>,
                        {
                            headers: responseHeaders(response.headers),
                            status: response.statusCode ?? 0,
                            statusText: response.statusMessage,
                        },
                    ),
                );
            },
        );

        request.on("error", reject);

        if (body) {
            request.write(body);
        }

        request.end();
    });
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
    envVar: BackendEnvVar,
    path: string,
    schema: ZodType<T>,
    options?: BackendFetchOptions,
): Promise<T> {
    const baseUrl = getBackendUrl(service, envVar);
    const agent =
        envVar === "WA_RESULTS_BACKEND_URL" ? resultsHttpsAgent() : undefined;
    const init = buildFetchInit(options, agent);

    let response: Response;
    try {
        response = await fetchBackend(buildBackendUrl(baseUrl, path), init);
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
    options?: BackendFetchOptions,
): Promise<T> {
    return backendJson(
        "results",
        "WA_RESULTS_BACKEND_URL",
        path,
        schema,
        options,
    );
}

export async function mlwhJson<T>(
    path: string,
    schema: ZodType<T>,
): Promise<T> {
    return backendJson("mlwh", "WA_MLWH_BACKEND_URL", path, schema);
}

export async function resultsRaw(
    path: string,
    options?: BackendFetchOptions,
): Promise<Response> {
    const init = buildFetchInit(options, resultsHttpsAgent());

    return fetchBackend(resultsBackendUrl(path), init);
}
