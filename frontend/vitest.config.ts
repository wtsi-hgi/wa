import { sep } from "node:path";
import { fileURLToPath } from "node:url";

import { defineConfig } from "vitest/config";

const frontendRoot = fileURLToPath(new URL("./", import.meta.url));
const aliasRoot = frontendRoot.endsWith(sep)
    ? frontendRoot
    : `${frontendRoot}${sep}`;

export default defineConfig({
    test: {
        environment: "node",
        globalSetup: ["./tests/integration/setup.ts"],
        hookTimeout: 120000,
        include: ["tests/**/*.test.ts"],
        testTimeout: 30000,
    },
    resolve: {
        alias: {
            "@": aliasRoot,
        },
    },
});
