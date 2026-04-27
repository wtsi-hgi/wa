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
        include: ["tests/*.test.ts"],
        setupFiles: ["./tests/vitest.setup.ts"],
    },
    resolve: {
        alias: {
            "@": aliasRoot,
        },
    },
});
