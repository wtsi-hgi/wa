// @vitest-environment jsdom

import { createElement } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
    clampFileListWidth,
    FILE_LIST_WIDTH_DEFAULT,
    FILE_LIST_WIDTH_MAX,
    FILE_LIST_WIDTH_MIN,
    useFileBrowserCompact,
    useFileBrowserListWidth,
    useStoredPreviewHeight,
} from "@/lib/file-browser-view-prefs";

function Prefs() {
    const [compact, setCompact] = useFileBrowserCompact();
    const [width, setWidth] = useFileBrowserListWidth();
    const [height, setHeight] = useStoredPreviewHeight(220);

    return createElement(
        "div",
        null,
        createElement("output", { "data-testid": "compact" }, String(compact)),
        createElement("output", { "data-testid": "width" }, String(width)),
        createElement("output", { "data-testid": "height" }, String(height)),
        createElement("button", { onClick: () => setCompact(true) }, "compact"),
        createElement("button", { onClick: () => setWidth(33.4) }, "width"),
        createElement("button", { onClick: () => setHeight(640.6) }, "height"),
    );
}

const value = (id: string) => screen.getByTestId(id).textContent;

afterEach(() => {
    cleanup();
    window.localStorage.clear();
    vi.restoreAllMocks();
});

describe("file browser view preferences", () => {
    it("defaults to the comfortable list, a quarter-width list and the fallback height", () => {
        render(createElement(Prefs));

        expect(value("compact")).toBe("false");
        expect(value("width")).toBe(String(FILE_LIST_WIDTH_DEFAULT));
        expect(value("height")).toBe("220");
    });

    it("stores changes, shares them with every reader and reads them back", () => {
        render(createElement(Prefs));
        render(createElement(Prefs));

        act(() => {
            screen.getAllByText("compact")[0]?.click();
            screen.getAllByText("width")[0]?.click();
            screen.getAllByText("height")[0]?.click();
        });

        expect(
            screen.getAllByTestId("compact").map((node) => node.textContent),
        ).toEqual(["true", "true"]);
        expect(
            screen.getAllByTestId("width").map((node) => node.textContent),
        ).toEqual(["33", "33"]);
        expect(window.localStorage.getItem("wa:file-browser:compact")).toBe(
            "1",
        );
        expect(window.localStorage.getItem("wa:file-browser:list-width")).toBe(
            "33",
        );
        expect(
            window.localStorage.getItem("wa:file-browser:preview-height"),
        ).toBe("641");

        cleanup();
        render(createElement(Prefs));

        expect(value("compact")).toBe("true");
        expect(value("width")).toBe("33");
        expect(value("height")).toBe("641");
    });

    it("clamps stored widths and ignores unreadable values", () => {
        window.localStorage.setItem("wa:file-browser:list-width", "95");
        window.localStorage.setItem("wa:file-browser:preview-height", "tall");
        render(createElement(Prefs));

        expect(value("width")).toBe(String(FILE_LIST_WIDTH_MAX));
        expect(value("height")).toBe("220");
        expect(clampFileListWidth(1)).toBe(FILE_LIST_WIDTH_MIN);
        expect(clampFileListWidth(Number.NaN)).toBe(FILE_LIST_WIDTH_DEFAULT);
    });

    it("keeps working when browser storage throws", () => {
        vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
            throw new Error("denied");
        });
        vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
            throw new Error("denied");
        });

        render(createElement(Prefs));

        expect(value("compact")).toBe("false");
        expect(() =>
            act(() => {
                screen.getByText("compact").click();
            }),
        ).not.toThrow();
    });
});
