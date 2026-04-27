// @vitest-environment jsdom

import { describe, expect, it } from "vitest";

describe("vitest React act environment", () => {
    it("enables the React act environment for jsdom tests", () => {
        expect(globalThis.IS_REACT_ACT_ENVIRONMENT).toBe(true);
    });
});
