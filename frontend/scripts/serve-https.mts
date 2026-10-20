import { readFileSync } from "node:fs";
import { createServer } from "node:https";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

// Serves the `next build` output over HTTPS. `next start` has no HTTPS option,
// so this wraps Next's production request handler in node:https; run-dev.sh
// --mode prod passes the same certificate and key it gives `next dev`.
//
// Usage: node scripts/serve-https.mts --port <port> --cert <pem> --key <pem>

const { values } = parseArgs({
    options: {
        port: { type: "string" },
        cert: { type: "string" },
        key: { type: "string" },
    },
});

const port = Number(values.port);

if (!Number.isInteger(port) || port < 1 || port > 65535) {
    console.error("serve-https: --port must be an integer from 1 to 65535");
    process.exit(1);
}

if (!values.cert || !values.key) {
    console.error("serve-https: --cert and --key are required");
    process.exit(1);
}

const tls = { cert: readFileSync(values.cert), key: readFileSync(values.key) };

// Next reads NODE_ENV when it loads, so set it before importing.
Object.assign(process.env, { NODE_ENV: "production" });

const { default: next } = await import("next");

const app = next({
    dev: false,
    dir: fileURLToPath(new URL("..", import.meta.url)),
    port,
});
const handle = app.getRequestHandler();

await app.prepare();

// As `next start` does, answer 500 rather than crash when Next's handler
// rejects.
const server = createServer(tls, (request, response) => {
    handle(request, response).catch((error: unknown) => {
        console.error(`serve-https: failed to handle ${request.url}`, error);
        if (response.headersSent) {
            response.destroy();
            return;
        }
        response.statusCode = 500;
        response.end("Internal Server Error");
    });
});

// Let in-flight requests finish; close() then exits once they have.
const shutdown = () => {
    server.close(() => process.exit(0));
    server.closeIdleConnections();
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);

server.listen(port, () => {
    console.log(
        `serve-https: ${process.env.NODE_ENV} build listening for HTTPS on port ${port}`,
    );
});
