// @vitest-environment jsdom

import { describe, expect, it } from "vitest";

type ReactActEnvironmentGlobal = typeof globalThis & {
    IS_REACT_ACT_ENVIRONMENT?: boolean;
};

describe("vitest React act environment", () => {
    it("enables the React act environment for jsdom tests", () => {
        expect(
            (globalThis as ReactActEnvironmentGlobal).IS_REACT_ACT_ENVIRONMENT,
        ).toBe(true);
    });
});
