// @vitest-environment jsdom

import { createElement } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppProviders } from "@/components/app-providers";

const { toastErrorMock, toastSuccessMock } = vi.hoisted(() => ({
    toastErrorMock: vi.fn(),
    toastSuccessMock: vi.fn(),
}));

vi.mock("sonner", () => ({
    Toaster: () => createElement("div", { "data-testid": "sonner-toaster" }),
    toast: {
        error: toastErrorMock,
        success: toastSuccessMock,
    },
}));

describe("bug 2 result ID copy chip", () => {
    const resultId =
        "result-2026-04-16-operator-1-pipeline-run-abcdef1234567890";
    const matchMediaStub = () => ({
        addEventListener: vi.fn(),
        addListener: vi.fn(),
        dispatchEvent: vi.fn(),
        matches: false,
        media: "",
        onchange: null,
        removeEventListener: vi.fn(),
        removeListener: vi.fn(),
    });

    afterEach(() => {
        document.body.innerHTML = "";
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it("renders a truncated label while preserving the full copy target", async () => {
        const { ResultIdCopyChip } =
            await import("@/app/(results)/results/[id]/result-id-copy-chip");

        vi.stubGlobal("matchMedia", matchMediaStub);

        render(
            createElement(
                AppProviders,
                undefined,
                createElement(ResultIdCopyChip, { resultId }),
            ),
        );

        const copyButton = screen.getByRole("button", {
            name: `Copy result ID ${resultId}`,
        });

        expect(copyButton.getAttribute("data-result-id-copy")).toBe(resultId);
        expect(copyButton.textContent).not.toContain(resultId);
        expect(copyButton.textContent).toContain("...");
    });

    it("copies the full result ID through the Clipboard API", async () => {
        const { ResultIdCopyChip } =
            await import("@/app/(results)/results/[id]/result-id-copy-chip");
        const writeTextMock = vi.fn().mockResolvedValue(undefined);

        vi.stubGlobal("matchMedia", matchMediaStub);
        vi.stubGlobal("navigator", {
            clipboard: {
                writeText: writeTextMock,
            },
        });

        render(
            createElement(
                AppProviders,
                undefined,
                createElement(ResultIdCopyChip, { resultId }),
            ),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: `Copy result ID ${resultId}`,
            }),
        );

        await waitFor(() => {
            expect(writeTextMock).toHaveBeenCalledWith(resultId);
        });

        expect(toastSuccessMock).toHaveBeenCalledWith("Result ID copied");
        expect(toastErrorMock).not.toHaveBeenCalled();
    });

    it("falls back to document copy when the Clipboard API is unavailable", async () => {
        const { ResultIdCopyChip } =
            await import("@/app/(results)/results/[id]/result-id-copy-chip");
        const execCommandMock = vi.fn().mockReturnValue(true);

        vi.stubGlobal("matchMedia", matchMediaStub);
        vi.stubGlobal("navigator", {});
        Object.defineProperty(document, "execCommand", {
            configurable: true,
            value: execCommandMock,
        });

        render(
            createElement(
                AppProviders,
                undefined,
                createElement(ResultIdCopyChip, { resultId }),
            ),
        );

        fireEvent.click(
            screen.getByRole("button", {
                name: `Copy result ID ${resultId}`,
            }),
        );

        await waitFor(() => {
            expect(execCommandMock).toHaveBeenCalledWith("copy");
        });

        expect(toastSuccessMock).toHaveBeenCalledWith("Result ID copied");
        expect(toastErrorMock).not.toHaveBeenCalled();
    });
});
