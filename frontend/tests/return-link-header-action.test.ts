// @vitest-environment jsdom

import { createElement, Fragment } from "react";
import { render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ReturnLinkHeaderAction } from "@/app/(results)/results/[id]/return-link-header-action";

describe("result detail return link header action", () => {
    afterEach(() => {
        document.body.innerHTML = "";
        vi.unstubAllGlobals();
    });

    it("portals the return link into the results header action slot", async () => {
        vi.stubGlobal(
            "requestAnimationFrame",
            (callback: FrameRequestCallback) => {
                callback(0);

                return 1;
            },
        );
        vi.stubGlobal("cancelAnimationFrame", vi.fn());

        const { container } = render(
            createElement(
                Fragment,
                undefined,
                createElement(
                    "header",
                    { "data-results-auth-bar": "true" },
                    createElement("div", {
                        "data-results-header-actions": "true",
                    }),
                    createElement("button", { type: "button" }, "Log in"),
                ),
                createElement(
                    "main",
                    undefined,
                    createElement(ReturnLinkHeaderAction, {
                        href: "/?requester=alice",
                        label: "Back to search results",
                    }),
                    createElement("section", {
                        "data-result-detail-summary": "true",
                    }),
                ),
            ),
        );

        await waitFor(() => {
            expect(
                document.querySelector(
                    '[data-results-header-actions="true"] a[data-return-link="true"]',
                ),
            ).not.toBeNull();
        });

        const headerLink = document.querySelector<HTMLAnchorElement>(
            '[data-results-header-actions="true"] a[data-return-link="true"]',
        );

        expect(headerLink?.getAttribute("href")).toBe("/?requester=alice");
        expect(headerLink?.textContent).toContain("Back to search results");
        expect(
            container.querySelector(
                'main [data-result-detail-summary="true"] a[data-return-link="true"]',
            ),
        ).toBeNull();
    });
});
