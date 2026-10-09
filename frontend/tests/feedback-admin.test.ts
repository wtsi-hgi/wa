import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
    defaultMLWHServerTokenBasename,
    feedbackAdmins,
    feedbackPageSize,
    isFeedbackAdmin,
    mlwhServerTokenPath,
    readMLWHServerToken,
} from "@/lib/feedback-admin";

const osMocks = vi.hoisted(() => ({
    homedir: vi.fn<() => string>(),
    userInfo: vi.fn<() => { username: string }>(),
}));

vi.mock("node:os", async (importOriginal) => {
    const actual = await importOriginal<typeof import("node:os")>();

    return {
        ...actual,
        homedir: osMocks.homedir,
        userInfo: osMocks.userInfo,
    };
});

const fakeHome = "/home/fake-feedback-user";

// processEnv types a partial env literal as NodeJS.ProcessEnv, whose Next.js
// augmentation otherwise requires NODE_ENV.
function processEnv(vars: Partial<NodeJS.ProcessEnv>): NodeJS.ProcessEnv {
    return vars as NodeJS.ProcessEnv;
}

beforeEach(() => {
    osMocks.homedir.mockReset().mockReturnValue(fakeHome);
    osMocks.userInfo.mockReset().mockReturnValue({ username: "os-user" });
});

afterEach(() => {
    vi.unstubAllEnvs();
});

describe("E2 feedback admin constants", () => {
    it("exports the default token basename and page size", () => {
        expect(defaultMLWHServerTokenBasename).toBe(".wa-mlwh-server.token");
        expect(feedbackPageSize).toBe(50);
    });
});

describe("E2 mlwhServerTokenPath", () => {
    it("resolves the token path like gas tokenStoragePath (test 1)", () => {
        expect(mlwhServerTokenPath(processEnv({ XDG_STATE_HOME: "/s" }))).toBe(
            "/s/.wa-mlwh-server.token",
        );
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "/s",
                    WA_MLWH_SERVER_TOKEN: ".t",
                }),
            ),
        ).toBe("/s/.t");
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "/s",
                    WA_MLWH_SERVER_TOKEN: "/abs/tok",
                }),
            ),
        ).toBe("/abs/tok");
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "/s",
                    WA_MLWH_SERVER_TOKEN: "a/b",
                }),
            ),
        ).toBe("/s/a/b");
    });

    it("trims WA_MLWH_SERVER_TOKEN and treats a blank value as unset", () => {
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "/s",
                    WA_MLWH_SERVER_TOKEN: "  .t \n",
                }),
            ),
        ).toBe("/s/.t");
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "/s",
                    WA_MLWH_SERVER_TOKEN: " /abs/tok ",
                }),
            ),
        ).toBe("/abs/tok");
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "/s",
                    WA_MLWH_SERVER_TOKEN: "   ",
                }),
            ),
        ).toBe("/s/.wa-mlwh-server.token");
    });

    it("falls back to os.homedir() without XDG_STATE_HOME (test 2)", () => {
        expect(mlwhServerTokenPath(processEnv({ HOME: "/not/used" }))).toBe(
            `${fakeHome}/.wa-mlwh-server.token`,
        );
        expect(
            mlwhServerTokenPath(
                processEnv({
                    XDG_STATE_HOME: "",
                    WA_MLWH_SERVER_TOKEN: ".t",
                }),
            ),
        ).toBe(`${fakeHome}/.t`);
        expect(osMocks.homedir).toHaveBeenCalled();
    });

    it("defaults env to process.env", () => {
        vi.stubEnv("XDG_STATE_HOME", "/from-process-env");
        vi.stubEnv("WA_MLWH_SERVER_TOKEN", ".process.token");

        expect(mlwhServerTokenPath()).toBe("/from-process-env/.process.token");
    });
});

describe("E2 readMLWHServerToken (test 3)", () => {
    let stateDir: string;

    beforeEach(async () => {
        stateDir = await mkdtemp(join(tmpdir(), "wa-feedback-admin-"));
    });

    afterEach(async () => {
        await rm(stateDir, { force: true, recursive: true });
    });

    it("returns the trimmed contents of the default token file", async () => {
        await writeFile(
            join(stateDir, ".wa-mlwh-server.token"),
            "tok\n",
            "utf8",
        );

        expect(
            readMLWHServerToken(processEnv({ XDG_STATE_HOME: stateDir })),
        ).toBe("tok");
    });

    it("reads the WA_MLWH_SERVER_TOKEN file, trimming surrounding space", async () => {
        const tokenPath = join(stateDir, "custom.token");
        await writeFile(tokenPath, "  s3cret-token \r\n", "utf8");
        await writeFile(
            join(stateDir, ".wa-mlwh-server.token"),
            "wrong\n",
            "utf8",
        );

        expect(
            readMLWHServerToken(
                processEnv({
                    XDG_STATE_HOME: "/nonexistent-state-dir",
                    WA_MLWH_SERVER_TOKEN: tokenPath,
                }),
            ),
        ).toBe("s3cret-token");
        expect(
            readMLWHServerToken(
                processEnv({
                    XDG_STATE_HOME: stateDir,
                    WA_MLWH_SERVER_TOKEN: "custom.token",
                }),
            ),
        ).toBe("s3cret-token");
    });

    it("returns null for a missing file", () => {
        expect(
            readMLWHServerToken(processEnv({ XDG_STATE_HOME: stateDir })),
        ).toBeNull();
    });

    it("returns null for an empty or whitespace-only file", async () => {
        const tokenPath = join(stateDir, ".wa-mlwh-server.token");

        await writeFile(tokenPath, "", "utf8");
        expect(
            readMLWHServerToken(processEnv({ XDG_STATE_HOME: stateDir })),
        ).toBeNull();

        await writeFile(tokenPath, " \n\t\n", "utf8");
        expect(
            readMLWHServerToken(processEnv({ XDG_STATE_HOME: stateDir })),
        ).toBeNull();
    });

    it("returns null for an unreadable path", async () => {
        await mkdir(join(stateDir, ".wa-mlwh-server.token"));

        expect(
            readMLWHServerToken(processEnv({ XDG_STATE_HOME: stateDir })),
        ).toBeNull();
    });

    it("defaults env to process.env", async () => {
        await writeFile(join(stateDir, "env.token"), "env-tok\n", "utf8");
        vi.stubEnv("XDG_STATE_HOME", stateDir);
        vi.stubEnv("WA_MLWH_SERVER_TOKEN", "env.token");

        expect(readMLWHServerToken()).toBe("env-tok");
    });
});

describe("E2 feedback admins", () => {
    it("parses WA_FEEDBACK_ADMINS and adds the OS user (test 4)", () => {
        const env = processEnv({ WA_FEEDBACK_ADMINS: " alice, ,bob " });

        expect(feedbackAdmins(env, "svc")).toEqual(
            new Set(["alice", "bob", "svc"]),
        );
        expect(isFeedbackAdmin("bob", env, "svc")).toBe(true);
        expect(isFeedbackAdmin("alice", env, "svc")).toBe(true);
        expect(isFeedbackAdmin("svc", env, "svc")).toBe(true);
        expect(isFeedbackAdmin("Bob", env, "svc")).toBe(false);
        expect(isFeedbackAdmin(" bob", env, "svc")).toBe(false);
        expect(isFeedbackAdmin("", env, "svc")).toBe(false);
        expect(isFeedbackAdmin(null, env, "svc")).toBe(false);
    });

    it("drops empty entries from leading, trailing, and doubled commas", () => {
        expect(
            feedbackAdmins(
                processEnv({ WA_FEEDBACK_ADMINS: ",,alice,,,bob," }),
                "svc",
            ),
        ).toEqual(new Set(["alice", "bob", "svc"]));
    });

    it("admits only the OS user when WA_FEEDBACK_ADMINS is unset (test 5)", () => {
        expect(feedbackAdmins(processEnv({}), "svc")).toEqual(new Set(["svc"]));
        expect(isFeedbackAdmin("svc", processEnv({}), "svc")).toBe(true);
        expect(isFeedbackAdmin("alice", processEnv({}), "svc")).toBe(false);
        expect(isFeedbackAdmin("", processEnv({}), "svc")).toBe(false);
    });

    it("defaults osUsername to userInfo().username only when omitted", () => {
        const env = processEnv({ WA_FEEDBACK_ADMINS: "alice" });

        expect(feedbackAdmins(env)).toEqual(new Set(["alice", "os-user"]));
        expect(isFeedbackAdmin("os-user", env)).toBe(true);

        osMocks.userInfo.mockClear();
        expect(feedbackAdmins(env, "svc")).toEqual(new Set(["alice", "svc"]));
        expect(isFeedbackAdmin("os-user", env, "svc")).toBe(false);
        expect(osMocks.userInfo).not.toHaveBeenCalled();
    });

    it("defaults env to process.env", () => {
        vi.stubEnv("WA_FEEDBACK_ADMINS", "carol, dave");

        expect(feedbackAdmins(undefined, "svc")).toEqual(
            new Set(["carol", "dave", "svc"]),
        );
        expect(isFeedbackAdmin("dave", undefined, "svc")).toBe(true);
    });

    it("survives a throwing userInfo() (test 6)", () => {
        osMocks.userInfo.mockImplementation(() => {
            throw new Error(
                "ENOENT: no such file or directory, uv_os_get_passwd",
            );
        });

        const env = processEnv({ WA_FEEDBACK_ADMINS: "alice" });

        expect(feedbackAdmins(env)).toEqual(new Set(["alice"]));
        expect(isFeedbackAdmin("alice", env)).toBe(true);
        expect(isFeedbackAdmin("svc", env)).toBe(false);
        expect(feedbackAdmins(processEnv({}))).toEqual(new Set());
        expect(isFeedbackAdmin("alice", processEnv({}))).toBe(false);
        expect(osMocks.userInfo).toHaveBeenCalled();
    });
});
