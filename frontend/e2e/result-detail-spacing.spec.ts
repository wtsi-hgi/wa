import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

type BoxSpacing = {
    border: EdgeSpacing;
    margin: EdgeSpacing;
    padding: EdgeSpacing;
};

type EdgeSpacing = {
    bottom: number;
    left: number;
    right: number;
    top: number;
};

type RectMetrics = {
    bottom: number;
    height: number;
    right: number;
    width: number;
    x: number;
    y: number;
};

type ResultDetailSpacingMetrics = {
    betweenBoxes: {
        leftEdgeDifference: number;
        rightEdgeDifference: number;
        verticalGap: number;
        widthDifference: number;
    };
    document: {
        clientWidth: number;
        scrollWidth: number;
    };
    fileBrowser: {
        firstDirectoryInsetFromBox: EdgeSpacing | null;
        header: {
            firstIconInsetFromBox: EdgeSpacing | null;
            insetFromBox: EdgeSpacing;
            rect: RectMetrics;
            spacing: BoxSpacing;
        };
        pageEdgeMargins: {
            left: number;
            right: number;
        };
        rect: RectMetrics;
        spacing: BoxSpacing;
        treeInner: {
            insetFromBox: EdgeSpacing;
            insetFromTreeShell: EdgeSpacing;
            rect: RectMetrics;
            spacing: BoxSpacing;
        } | null;
        treeShell: {
            insetFromBox: EdgeSpacing;
            rect: RectMetrics;
            spacing: BoxSpacing;
        } | null;
    };
    infoBox: {
        firstContentInsetFromBox: EdgeSpacing | null;
        innerWrapper: {
            insetFromBox: EdgeSpacing;
            rect: RectMetrics;
            spacing: BoxSpacing;
        } | null;
        pageEdgeMargins: {
            left: number;
            right: number;
        };
        rect: RectMetrics;
        spacing: BoxSpacing;
    };
    main: {
        contentWidthApprox: number;
        edgeMargins: {
            left: number;
            right: number;
        };
        rect: RectMetrics;
        spacing: BoxSpacing;
    };
    viewport: {
        deviceScaleFactor: number;
        height: number;
        width: number;
    };
};

const evidenceDir = path.resolve(process.cwd(), "..", ".tmp", "agent");
const postFixScreenshotPath = path.join(
    evidenceDir,
    "bug2-results-detail-edge-margin-postfix-1440.png",
);
const postFixMeasurementPath = path.join(
    evidenceDir,
    "bug2-results-detail-spacing-postfix-measurements.json",
);

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

function recentRows(page: Page) {
    return page
        .locator('tbody tr[data-result-row="true"]')
        .filter({ hasNotText: "seqmeta/rendering-repro" });
}

async function seededResultDetailUrl(page: Page): Promise<string> {
    await page.goto("/");
    await expect(page.getByText("Recent registrations")).toBeVisible();
    await expect
        .poll(async () => recentRows(page).count())
        .toBeGreaterThanOrEqual(4);

    const resultLink = page.getByRole("link", { name: "nf-core/rnaseq" });
    const href = await resultLink.first().getAttribute("href");

    return new URL(href ?? "/results/", page.url()).toString();
}

async function waitForResultDetail(page: Page): Promise<void> {
    await expect(
        page.getByRole("heading", { level: 1, name: "nf-core/rnaseq" }),
    ).toBeVisible({ timeout: 30000 });
    await expect(
        page.locator('[data-result-detail-summary="true"]'),
    ).toBeVisible();
    await expect(page.locator('[data-file-browser="true"]')).toBeVisible();
}

async function measureResultDetailSpacing(
    page: Page,
): Promise<ResultDetailSpacingMetrics> {
    return page.evaluate(() => {
        function requiredElement(selector: string): HTMLElement {
            const element = document.querySelector(selector);

            if (!(element instanceof HTMLElement)) {
                throw new Error(`Missing layout element: ${selector}`);
            }

            return element;
        }

        function rectMetrics(element: Element): RectMetrics {
            const rect = element.getBoundingClientRect();

            return {
                bottom: Math.round(rect.bottom),
                height: Math.round(rect.height),
                right: Math.round(rect.right),
                width: Math.round(rect.width),
                x: Math.round(rect.x),
                y: Math.round(rect.y),
            };
        }

        function spacing(element: Element): BoxSpacing {
            const styles = window.getComputedStyle(element);
            const edge = (property: "border" | "margin" | "padding") => ({
                bottom: Number.parseFloat(
                    styles.getPropertyValue(`${property}-bottom-width`) ||
                        styles.getPropertyValue(`${property}-bottom`),
                ),
                left: Number.parseFloat(
                    styles.getPropertyValue(`${property}-left-width`) ||
                        styles.getPropertyValue(`${property}-left`),
                ),
                right: Number.parseFloat(
                    styles.getPropertyValue(`${property}-right-width`) ||
                        styles.getPropertyValue(`${property}-right`),
                ),
                top: Number.parseFloat(
                    styles.getPropertyValue(`${property}-top-width`) ||
                        styles.getPropertyValue(`${property}-top`),
                ),
            });

            return {
                border: edge("border"),
                margin: edge("margin"),
                padding: edge("padding"),
            };
        }

        function inset(outer: Element, inner: Element): EdgeSpacing {
            const outerRect = outer.getBoundingClientRect();
            const innerRect = inner.getBoundingClientRect();

            return {
                bottom: Math.round(outerRect.bottom - innerRect.bottom),
                left: Math.round(innerRect.left - outerRect.left),
                right: Math.round(outerRect.right - innerRect.right),
                top: Math.round(innerRect.top - outerRect.top),
            };
        }

        function optionalInset(
            outer: Element,
            inner: Element | null,
        ): EdgeSpacing | null {
            return inner instanceof Element ? inset(outer, inner) : null;
        }

        const main = requiredElement("main");
        const infoBox = requiredElement('[data-result-detail-summary="true"]');
        const infoInner = infoBox.firstElementChild;
        const infoFirstContent = infoBox.querySelector("[data-return-link]");
        const fileBrowser = requiredElement('[data-file-browser="true"]');
        const fileHeader = requiredElement('[data-file-browser-header="true"]');
        const fileHeaderIcon = fileHeader.querySelector("svg");
        const treeShell = fileBrowser.querySelector("[data-preview-mode]");
        const treeInner = treeShell?.firstElementChild ?? null;
        const firstDirectory = fileBrowser.querySelector(
            "[data-directory-path]",
        );
        const mainRect = rectMetrics(main);
        const infoRect = rectMetrics(infoBox);
        const browserRect = rectMetrics(fileBrowser);
        const mainSpacing = spacing(main);

        return {
            betweenBoxes: {
                leftEdgeDifference: Math.abs(infoRect.x - browserRect.x),
                rightEdgeDifference: Math.abs(
                    infoRect.right - browserRect.right,
                ),
                verticalGap: browserRect.y - infoRect.bottom,
                widthDifference: Math.abs(infoRect.width - browserRect.width),
            },
            document: {
                clientWidth: document.documentElement.clientWidth,
                scrollWidth: document.documentElement.scrollWidth,
            },
            fileBrowser: {
                firstDirectoryInsetFromBox: optionalInset(
                    fileBrowser,
                    firstDirectory,
                ),
                header: {
                    firstIconInsetFromBox: optionalInset(
                        fileBrowser,
                        fileHeaderIcon,
                    ),
                    insetFromBox: inset(fileBrowser, fileHeader),
                    rect: rectMetrics(fileHeader),
                    spacing: spacing(fileHeader),
                },
                pageEdgeMargins: {
                    left: browserRect.x,
                    right: window.innerWidth - browserRect.right,
                },
                rect: browserRect,
                spacing: spacing(fileBrowser),
                treeInner:
                    treeShell instanceof HTMLElement &&
                    treeInner instanceof HTMLElement
                        ? {
                              insetFromBox: inset(fileBrowser, treeInner),
                              insetFromTreeShell: inset(treeShell, treeInner),
                              rect: rectMetrics(treeInner),
                              spacing: spacing(treeInner),
                          }
                        : null,
                treeShell:
                    treeShell instanceof HTMLElement
                        ? {
                              insetFromBox: inset(fileBrowser, treeShell),
                              rect: rectMetrics(treeShell),
                              spacing: spacing(treeShell),
                          }
                        : null,
            },
            infoBox: {
                firstContentInsetFromBox: optionalInset(
                    infoBox,
                    infoFirstContent,
                ),
                innerWrapper:
                    infoInner instanceof HTMLElement
                        ? {
                              insetFromBox: inset(infoBox, infoInner),
                              rect: rectMetrics(infoInner),
                              spacing: spacing(infoInner),
                          }
                        : null,
                pageEdgeMargins: {
                    left: infoRect.x,
                    right: window.innerWidth - infoRect.right,
                },
                rect: infoRect,
                spacing: spacing(infoBox),
            },
            main: {
                contentWidthApprox:
                    mainRect.width -
                    mainSpacing.padding.left -
                    mainSpacing.padding.right,
                edgeMargins: {
                    left: mainRect.x,
                    right: window.innerWidth - mainRect.right,
                },
                rect: mainRect,
                spacing: mainSpacing,
            },
            viewport: {
                deviceScaleFactor: window.devicePixelRatio,
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    });
}

test("keeps result detail boxes wider with consistent compact internal padding", async ({
    page,
}) => {
    const detailUrl = await seededResultDetailUrl(page);
    const viewports = [
        { width: 1440, height: 1200 },
        { width: 1280, height: 1000 },
        { width: 1024, height: 900 },
        { width: 390, height: 900 },
    ];
    const measurements: ResultDetailSpacingMetrics[] = [];

    mkdirSync(evidenceDir, { recursive: true });

    for (const viewport of viewports) {
        await page.setViewportSize(viewport);
        await page.goto(detailUrl, { waitUntil: "domcontentloaded" });
        await waitForResultDetail(page);

        const metrics = await measureResultDetailSpacing(page);
        measurements.push(metrics);

        if (viewport.width === 1440) {
            await page.screenshot({
                fullPage: true,
                path: postFixScreenshotPath,
            });
        }
    }

    writeFileSync(
        postFixMeasurementPath,
        `${JSON.stringify(
            {
                fixture: "seeded nf-core/rnaseq result",
                measuredAt: new Date().toISOString(),
                route: new URL(detailUrl).pathname,
                screenshotPath: postFixScreenshotPath,
                viewports: measurements,
            },
            null,
            2,
        )}\n`,
    );

    const desktop = measurements.find(
        (metrics) => metrics.viewport.width === 1440,
    );
    const tablet = measurements.find(
        (metrics) => metrics.viewport.width === 1280,
    );
    const compact = measurements.find(
        (metrics) => metrics.viewport.width === 1024,
    );

    if (!desktop || !tablet || !compact) {
        throw new Error("Missing measured desktop/tablet layout");
    }

    expect(desktop.infoBox.pageEdgeMargins.left).toBeLessThanOrEqual(88);
    expect(desktop.infoBox.pageEdgeMargins.right).toBeLessThanOrEqual(88);
    expect(desktop.infoBox.rect.width).toBeGreaterThanOrEqual(1270);
    expect(tablet.infoBox.pageEdgeMargins.left).toBeLessThanOrEqual(36);
    expect(tablet.infoBox.pageEdgeMargins.right).toBeLessThanOrEqual(36);
    expect(tablet.infoBox.rect.width).toBeGreaterThanOrEqual(1208);
    expect(compact.infoBox.pageEdgeMargins.left).toBeLessThanOrEqual(36);
    expect(compact.infoBox.pageEdgeMargins.right).toBeLessThanOrEqual(36);
    expect(compact.infoBox.rect.width).toBeGreaterThanOrEqual(952);

    for (const metrics of measurements) {
        const infoPadding = metrics.infoBox.innerWrapper?.spacing.padding;
        const filePadding = metrics.fileBrowser.spacing.padding;
        const treeShellPadding = metrics.fileBrowser.treeShell?.spacing.padding;
        const infoInset = metrics.infoBox.firstContentInsetFromBox;
        const firstDirectoryInset =
            metrics.fileBrowser.firstDirectoryInsetFromBox;
        const fileHeaderInset =
            metrics.fileBrowser.header.firstIconInsetFromBox;

        expect(metrics.document.scrollWidth).toBeLessThanOrEqual(
            metrics.document.clientWidth,
        );
        expect(metrics.betweenBoxes.leftEdgeDifference).toBeLessThanOrEqual(1);
        expect(metrics.betweenBoxes.rightEdgeDifference).toBeLessThanOrEqual(1);
        expect(metrics.betweenBoxes.widthDifference).toBeLessThanOrEqual(1);

        if (
            !infoPadding ||
            !treeShellPadding ||
            !infoInset ||
            !firstDirectoryInset ||
            !fileHeaderInset
        ) {
            throw new Error("Missing measured internal spacing");
        }

        expect(infoPadding.left).toBeLessThanOrEqual(filePadding.left);
        expect(infoPadding.right).toBeLessThanOrEqual(filePadding.right);
        expect(infoPadding.top).toBeLessThanOrEqual(filePadding.top);
        expect(infoPadding.bottom).toBeLessThanOrEqual(filePadding.bottom);
        expect(filePadding.left).toBeLessThanOrEqual(16);
        expect(filePadding.right).toBeLessThanOrEqual(16);
        expect(filePadding.top).toBeLessThanOrEqual(16);
        expect(filePadding.bottom).toBeLessThanOrEqual(16);
        expect(treeShellPadding.left).toBeLessThanOrEqual(4);
        expect(treeShellPadding.right).toBeLessThanOrEqual(4);
        expect(
            firstDirectoryInset.left - fileHeaderInset.left,
        ).toBeLessThanOrEqual(6);
        expect(
            Math.abs(infoInset.left - fileHeaderInset.left),
        ).toBeLessThanOrEqual(2);
        expect(
            Math.abs(infoInset.top - fileHeaderInset.top),
        ).toBeLessThanOrEqual(4);
    }
});
