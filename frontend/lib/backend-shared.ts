export const resultsAuthCookieName = "wa_results_jwt";

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
