import fs from "node:fs";
import http from "node:http";

const port = Number(process.argv[2] ?? process.env.WA_TEST_FRONTEND_PORT ?? 3000);
const snapshotPath = process.env.WA_RUN_DEV_ENV_SNAPSHOT;

if (!snapshotPath) {
    console.error("WA_RUN_DEV_ENV_SNAPSHOT is required");
    process.exit(1);
}

fs.writeFileSync(
    snapshotPath,
    JSON.stringify(
        {
            WA_RESULTS_BACKEND_URL: process.env.WA_RESULTS_BACKEND_URL ?? "",
            WA_SEQMETA_BACKEND_URL: process.env.WA_SEQMETA_BACKEND_URL ?? "",
            WA_RESULTS_DB_PATH: process.env.WA_RESULTS_DB_PATH ?? "",
            WA_DEV_ALLOWED_ORIGINS: process.env.WA_DEV_ALLOWED_ORIGINS ?? "",
        },
        null,
        2,
    ),
);

const server = http.createServer((request, response) => {
    if (request.url === "/api/health") {
        response.writeHead(200, { "content-type": "application/json" });
        response.end(JSON.stringify({ status: "healthy" }));
        return;
    }

    response.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    response.end("run-dev frontend stub");
});

const shutdown = () => {
    server.close(() => process.exit(0));
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);

server.listen(port, "127.0.0.1");
