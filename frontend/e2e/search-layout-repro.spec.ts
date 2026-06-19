import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test, type Page } from "@playwright/test";

import { installResultsAuthCookie } from "./results-auth-helpers";

const repoRoot = path.resolve(process.cwd(), "..");
const evidenceDir = path.join(repoRoot, ".tmp", "agent");

type RectMetric = {
    bottom: number;
    height: number;
    left: number;
    right: number;
    top: number;
    width: number;
};

type GenericSearchMetric = {
    button: RectMetric;
    buttonCenterDeltaFromControl: {
        x: number;
        y: number;
    };
    control: RectMetric;
    form: RectMetric;
    hasPermanentGrid: boolean;
    icon: RectMetric;
    iconCenterDeltaFromButton: {
        x: number;
        y: number;
    };
    input: RectMetric;
};

type SearchLayoutMetric = {
    genericSearch: GenericSearchMetric;
    viewport: {
        height: number;
        width: number;
    };
};

test.beforeEach(async ({ context }) => {
    await installResultsAuthCookie(context);
});

async function collectSearchLayoutMetric(
    page: Page,
): Promise<SearchLayoutMetric> {
    return page.evaluate(() => {
        const toBrowserRect = (element: Element): RectMetric => {
            const rect = element.getBoundingClientRect();

            return {
                bottom: rect.bottom,
                height: rect.height,
                left: rect.left,
                right: rect.right,
                top: rect.top,
                width: rect.width,
            };
        };
        const center = (rect: RectMetric) => ({
            x: rect.left + rect.width / 2,
            y: rect.top + rect.height / 2,
        });
        const input = document.querySelector<HTMLInputElement>(
            '[data-generic-search-input="true"]',
        );

        if (!input) {
            throw new Error("Missing generic search input");
        }

        const form = input.closest("form");
        const control = input.parentElement;
        const button = form?.querySelector<HTMLButtonElement>(
            'button[aria-label="Add generic search match"]',
        );
        const icon = button?.querySelector("svg");

        if (
            !(form instanceof HTMLElement) ||
            !(control instanceof HTMLElement) ||
            !(button instanceof HTMLButtonElement) ||
            !(icon instanceof SVGElement)
        ) {
            throw new Error("Missing generic search measurement target");
        }

        const buttonRect = toBrowserRect(button);
        const controlRect = toBrowserRect(control);
        const iconRect = toBrowserRect(icon);
        const buttonCenter = center(buttonRect);
        const controlCenter = center(controlRect);
        const iconCenter = center(iconRect);

        return {
            genericSearch: {
                button: buttonRect,
                buttonCenterDeltaFromControl: {
                    x: Number((buttonCenter.x - controlCenter.x).toFixed(3)),
                    y: Number((buttonCenter.y - controlCenter.y).toFixed(3)),
                },
                control: controlRect,
                form: toBrowserRect(form),
                hasPermanentGrid: Boolean(
                    document.querySelector(
                        '[data-search-builder-permanent-fields="true"]',
                    ),
                ),
                icon: iconRect,
                iconCenterDeltaFromButton: {
                    x: Number((iconCenter.x - buttonCenter.x).toFixed(3)),
                    y: Number((iconCenter.y - buttonCenter.y).toFixed(3)),
                },
                input: toBrowserRect(input),
            },
            viewport: {
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    });
}

test("keeps seeded dashboard generic search compact at narrow tablet widths", async ({
    page,
}) => {
    const metrics: SearchLayoutMetric[] = [];

    mkdirSync(evidenceDir, { recursive: true });

    for (const width of [760, 767, 1000]) {
        await page.setViewportSize({ width, height: 900 });
        await page.goto("/");

        await expect(
            page.locator('[data-search-builder="true"]'),
        ).toBeVisible();
        await expect(page.getByText("Latest result sets")).toBeVisible();

        const metric = await collectSearchLayoutMetric(page);
        const screenshotPath = path.join(
            evidenceDir,
            `search-layout-dev-fixtures-postfix-${width}.png`,
        );

        await page.screenshot({
            animations: "disabled",
            fullPage: true,
            path: screenshotPath,
        });
        writeFileSync(
            screenshotPath.replace(/\.png$/, ".json"),
            `${JSON.stringify({ ...metric, screenshotPath }, null, 2)}\n`,
        );
        metrics.push(metric);

        expect.soft(metric.genericSearch.hasPermanentGrid).toBe(false);
        expect
            .soft(metric.genericSearch.control.width)
            .toBeGreaterThanOrEqual(300);
        expect
            .soft(metric.genericSearch.control.height)
            .toBeGreaterThanOrEqual(40);
        expect
            .soft(metric.genericSearch.control.height)
            .toBeLessThanOrEqual(48);
        expect
            .soft(Math.abs(metric.genericSearch.iconCenterDeltaFromButton.x))
            .toBeLessThanOrEqual(0.25);
        expect
            .soft(Math.abs(metric.genericSearch.iconCenterDeltaFromButton.y))
            .toBeLessThanOrEqual(0.25);
        expect
            .soft(Math.abs(metric.genericSearch.buttonCenterDeltaFromControl.y))
            .toBeLessThanOrEqual(0.25);
    }

    writeFileSync(
        path.join(evidenceDir, "search-layout-dev-fixtures-postfix.json"),
        `${JSON.stringify({ metrics }, null, 2)}\n`,
    );
});
