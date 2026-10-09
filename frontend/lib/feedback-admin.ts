import { readFileSync } from "node:fs";
import { homedir, userInfo } from "node:os";
import { isAbsolute, join } from "node:path";

export { feedbackPageSize } from "@/lib/contracts";

export const defaultMLWHServerTokenBasename = ".wa-mlwh-server.token";

// mlwhServerTokenPath mirrors gas tokenStoragePath, as used by wa mlwh serve:
// an absolute WA_MLWH_SERVER_TOKEN is used as-is, and any other value (or the
// default basename when it is blank) is joined under gas.TokenDir(), which is
// XDG_STATE_HOME, falling back to the home directory.
export function mlwhServerTokenPath(
    env: NodeJS.ProcessEnv = process.env,
): string {
    const token =
        env.WA_MLWH_SERVER_TOKEN?.trim() || defaultMLWHServerTokenBasename;

    if (isAbsolute(token)) {
        return token;
    }

    return join(env.XDG_STATE_HOME || homedir(), token);
}

// readMLWHServerToken returns the trimmed token file contents, or null when
// the file is absent, unreadable, or blank.
export function readMLWHServerToken(
    env: NodeJS.ProcessEnv = process.env,
): string | null {
    let contents: string;

    try {
        contents = readFileSync(mlwhServerTokenPath(env), "utf8");
    } catch {
        return null;
    }

    return contents.trim() || null;
}

// defaultOSUsername returns the server process's OS username, or undefined
// when the uid has no passwd entry and userInfo() throws.
function defaultOSUsername(): string | undefined {
    try {
        return userInfo().username;
    } catch {
        return undefined;
    }
}

export function feedbackAdmins(
    env: NodeJS.ProcessEnv = process.env,
    osUsername?: string,
): Set<string> {
    const admins = new Set(
        (env.WA_FEEDBACK_ADMINS ?? "")
            .split(",")
            .map((entry) => entry.trim())
            .filter((entry) => entry !== ""),
    );
    const owner = osUsername ?? defaultOSUsername();

    if (owner) {
        admins.add(owner);
    }

    return admins;
}

export function isFeedbackAdmin(
    username: string | null,
    env: NodeJS.ProcessEnv = process.env,
    osUsername?: string,
): boolean {
    return username !== null && feedbackAdmins(env, osUsername).has(username);
}
