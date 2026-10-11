import path from "node:path";

// Playwright's run-dev stack keeps every file it writes (dev TLS key and cert,
// test state and server token, built `wa` binary and link, ephemeral DBs,
// logs, the port cache and per-worker CLI state) in this directory, so
// `make test` leaves a verify-wa or `make dev` stack in the same checkout
// alone. `make clean-test-tmp` deletes it.
export const e2eRunDir = path.resolve(process.cwd(), "..", ".tmp", "e2e");
