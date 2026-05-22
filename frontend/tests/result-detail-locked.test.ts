// @vitest-environment jsdom

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";

import { BackendRequestError } from "@/lib/backend-client";

const fetchFilesMock = vi.fn();
const fetchResultMock = vi.fn();

vi.mock("@/app/(results)/actions", () => ({
    fetchFiles: fetchFilesMock,
    fetchResult: fetchResultMock,
}));

function lockedError(resultId: string): BackendRequestError {
    return new BackendRequestError(403, {
        error: "locked",
        locked: true,
        result_id: resultId,
        message: "You do not have access to this result set",
    });
}

async function renderLockedDetail(
    id = "abc",
    searchParams?: { returnTo?: string },
): Promise<HTMLElement> {
    fetchResultMock.mockRejectedValue(lockedError(id));

    const pageModule =
        await import("@/app/(results)/results/[id]/page-content");
    const Page = pageModule.ResultDetailPageContent;
    const markup = renderToStaticMarkup(
        await Page({
            id,
            searchParams,
        }),
    );
    const container = document.createElement("div");

    container.innerHTML = markup;

    return container;
}

describe("E4 locked result detail", () => {
    afterEach(() => {
        document.body.innerHTML = "";
        fetchFilesMock.mockReset();
        fetchResultMock.mockReset();
    });

    it("renders only the locked state and dashboard return link for a locked fetchResult response", async () => {
        const container = await renderLockedDetail("result-locked", {
            returnTo: "/?requester=alice",
        });

        expect(
            container.querySelector('[data-locked-detail-icon="true"]'),
        ).not.toBeNull();
        expect(container.textContent).toContain(
            "You do not have access to this result set",
        );
        expect(
            container
                .querySelector('[data-locked-result-detail="true"]')
                ?.querySelector('a[data-return-link="true"]'),
        ).toBeNull();
        expect(
            container.querySelector('a[data-return-link="true"]'),
        ).toBeNull();
        expect(
            container.querySelector('[data-result-detail-summary="true"]'),
        ).toBeNull();
        expect(container.textContent).not.toContain("Registered files");
        expect(fetchFilesMock).not.toHaveBeenCalled();
    });

    it("renders the locked state for anonymous direct navigation to a public locked detail response", async () => {
        const container = await renderLockedDetail("abc");

        expect(fetchResultMock).toHaveBeenCalledWith("abc");
        expect(fetchFilesMock).not.toHaveBeenCalled();
        expect(
            container.querySelector('[data-locked-result-detail="true"]'),
        ).not.toBeNull();
        expect(
            container
                .querySelector('[data-locked-result-detail="true"]')
                ?.querySelector('a[data-return-link="true"]'),
        ).toBeNull();
        expect(
            container.querySelector('a[data-return-link="true"]'),
        ).toBeNull();
    });
});
