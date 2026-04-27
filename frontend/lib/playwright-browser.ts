import { accessSync, constants, readdirSync } from "node:fs";
import path from "node:path";

type ResolveChromiumExecutableOptions = {
    env?: NodeJS.ProcessEnv;
    platform?: NodeJS.Platform;
    isExecutable?: (candidate: string) => boolean;
    listDirectory?: (directory: string) => string[];
};

function defaultIsExecutable(candidate: string): boolean {
    try {
        accessSync(candidate, constants.X_OK);
        return true;
    } catch {
        return false;
    }
}

function defaultListDirectory(directory: string): string[] {
    try {
        return readdirSync(directory, { withFileTypes: true })
            .filter((entry) => entry.isDirectory())
            .map((entry) => entry.name);
    } catch {
        return [];
    }
}

function chromiumBundleExecutableNames(platform: NodeJS.Platform): string[] {
    switch (platform) {
        case "darwin":
            return [
                "chrome-mac/Chromium.app/Contents/MacOS/Chromium",
                "chrome-headless-shell-mac/Chromium.app/Contents/MacOS/Chromium",
            ];
        case "win32":
            return [
                "chrome-win/chrome.exe",
                "chrome-win64/chrome.exe",
                "chrome-headless-shell-win64/chrome-headless-shell.exe",
            ];
        default:
            return [
                "chrome-linux64/chrome",
                "chrome-linux/chrome",
                "chrome-headless-shell-linux64/chrome-headless-shell",
            ];
    }
}

function chromiumPathExecutableNames(platform: NodeJS.Platform): string[] {
    switch (platform) {
        case "darwin":
            return ["Chromium", "Google Chrome for Testing", "Google Chrome"];
        case "win32":
            return ["chrome.exe", "msedge.exe"];
        default:
            return [
                "chromium",
                "chromium-browser",
                "google-chrome",
                "google-chrome-stable",
                "chrome",
            ];
    }
}

function chromiumBundleDirectories(
    browsersPath: string,
    listDirectory: (directory: string) => string[],
): string[] {
    return listDirectory(browsersPath)
        .filter(
            (entry) =>
                /^chromium-\d+$/.test(entry) ||
                /^chromium_headless_shell-\d+$/.test(entry),
        )
        .sort((left, right) =>
            right.localeCompare(left, undefined, { numeric: true }),
        );
}

export function resolveChromiumExecutablePath(
    options: ResolveChromiumExecutableOptions = {},
): string | undefined {
    const env = options.env ?? process.env;
    const platform = options.platform ?? process.platform;
    const isExecutable = options.isExecutable ?? defaultIsExecutable;
    const listDirectory = options.listDirectory ?? defaultListDirectory;
    const explicitExecutablePath = env.WA_PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;

    if (explicitExecutablePath && isExecutable(explicitExecutablePath)) {
        return explicitExecutablePath;
    }

    const browsersPath = env.PLAYWRIGHT_BROWSERS_PATH;

    if (browsersPath) {
        for (const directory of chromiumBundleDirectories(
            browsersPath,
            listDirectory,
        )) {
            for (const executableName of chromiumBundleExecutableNames(
                platform,
            )) {
                const candidate = path.join(
                    browsersPath,
                    directory,
                    executableName,
                );

                if (isExecutable(candidate)) {
                    return candidate;
                }
            }
        }
    }

    const pathEntries = env.PATH?.split(path.delimiter).filter(Boolean) ?? [];

    for (const directory of pathEntries) {
        for (const executableName of chromiumPathExecutableNames(platform)) {
            const candidate = path.join(directory, executableName);

            if (isExecutable(candidate)) {
                return candidate;
            }
        }
    }

    return undefined;
}
