import path from "node:path";
import { pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

import { resolveChromiumExecutablePath } from "@/lib/playwright-browser";

const frontendRoot = process.cwd();

function buildEnv(
    overrides: Record<string, string | undefined>,
): Record<string, string | undefined> {
    return {
        ...process.env,
        ...overrides,
    };
}

describe("Playwright browser resolution", () => {
    it("prefers the configured Playwright browser cache over the default cache", () => {
        const configuredBrowsersPath = "/opt/playwright-browsers";
        const expectedExecutable = path.join(
            configuredBrowsersPath,
            "chromium-1217",
            "chrome-linux64",
            "chrome",
        );

        const resolved = resolveChromiumExecutablePath({
            env: buildEnv({
                PATH: "/usr/bin",
                PLAYWRIGHT_BROWSERS_PATH: configuredBrowsersPath,
            }),
            platform: "linux",
            isExecutable: (candidate) => candidate === expectedExecutable,
            listDirectory: (directory) =>
                directory === configuredBrowsersPath
                    ? ["chromium-1217", "chromium_headless_shell-1217"]
                    : [],
        });

        expect(resolved).toBe(expectedExecutable);
    });

    it("resolves a chromium headless shell bundle from the configured Playwright cache", () => {
        const configuredBrowsersPath = "/opt/playwright-browsers";
        const expectedExecutable = path.join(
            configuredBrowsersPath,
            "chromium_headless_shell-1217",
            "chrome-headless-shell-linux64",
            "chrome-headless-shell",
        );

        const resolved = resolveChromiumExecutablePath({
            env: buildEnv({
                PATH: "/usr/bin",
                PLAYWRIGHT_BROWSERS_PATH: configuredBrowsersPath,
            }),
            platform: "linux",
            isExecutable: (candidate) => candidate === expectedExecutable,
            listDirectory: (directory) =>
                directory === configuredBrowsersPath
                    ? ["chromium_headless_shell-1217"]
                    : [],
        });

        expect(resolved).toBe(expectedExecutable);
    });

    it("falls back to a chromium binary on PATH when no browser cache is configured", () => {
        const expectedExecutable = "/usr/local/bin/chromium";

        const resolved = resolveChromiumExecutablePath({
            env: buildEnv({
                PATH: "/usr/local/bin:/usr/bin",
            }),
            platform: "linux",
            isExecutable: (candidate) => candidate === expectedExecutable,
            listDirectory: () => [],
        });

        expect(resolved).toBe(expectedExecutable);
    });

    it("wires the resolved executable path into the Playwright config", async () => {
        const previousExecutablePath =
            process.env.WA_PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
        process.env.WA_PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH = "/bin/true";

        try {
            const configModule = (await import(
                `${pathToFileURL(path.join(frontendRoot, "playwright.config.ts")).href}?test=${Date.now()}`
            )) as {
                default: {
                    use?: {
                        launchOptions?: {
                            executablePath?: string;
                        };
                    };
                };
            };

            expect(
                configModule.default.use?.launchOptions?.executablePath,
            ).toBe("/bin/true");
        } finally {
            if (previousExecutablePath === undefined) {
                delete process.env.WA_PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
            } else {
                process.env.WA_PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH =
                    previousExecutablePath;
            }
        }
    });

    it("extends the web server timeout to cover the larger frontend health wait budget", async () => {
        const configModule = (await import(
            `${pathToFileURL(path.join(frontendRoot, "playwright.config.ts")).href}?test=${Date.now()}`
        )) as {
            default: {
                webServer?: {
                    command?: string;
                    timeout?: number;
                };
            };
        };

        expect(configModule.default.webServer?.command).toContain(
            'WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS="720"',
        );
        expect(configModule.default.webServer?.command).toContain(
            'WA_RUN_DEV_FRONTEND_HEALTH_URL="http://127.0.0.1:',
        );
        expect(configModule.default.webServer?.command).toContain(
            '/api/health"',
        );
        expect(configModule.default.webServer?.command).toContain(
            'WA_RUN_DEV_FRONTEND_DEV_CMD="bash -lc \\"pnpm exec next build && exec pnpm exec next start --port ',
        );
        expect(configModule.default.webServer?.timeout).toBe(330_000);
    });
});
