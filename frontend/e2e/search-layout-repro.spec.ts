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

type PermanentFieldMetric = {
    button: RectMetric;
    buttonCenterDeltaFromControl: {
        x: number;
        y: number;
    };
    control: RectMetric;
    field: RectMetric;
    icon: RectMetric;
    iconCenterDeltaFromButton: {
        x: number;
        y: number;
    };
    input: RectMetric;
    key: string;
    label: string;
};

type SearchLayoutMetric = {
    compactColumnWidth: number;
    columnGap: number;
    fieldCount: number;
    fields: PermanentFieldMetric[];
    grid: RectMetric;
    rowCount: number;
    rows: Array<{
        fieldCount: number;
        top: number;
    }>;
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
        const searchGrid = document.querySelector<HTMLElement>(
            '[data-search-builder-permanent-fields="true"]',
        );

        if (!searchGrid) {
            throw new Error("Missing permanent search fields grid");
        }

        const fields: PermanentFieldMetric[] = Array.from(
            searchGrid.querySelectorAll("form"),
        ).map((form) => {
            const input = form.querySelector<HTMLInputElement>(
                "[data-permanent-filter-input]",
            );
            const button = form.querySelector<HTMLButtonElement>(
                'button[type="submit"]',
            );
            const label = form.querySelector("label");
            const control = input?.parentElement;
            const icon = button?.querySelector("svg");

            if (
                !input ||
                !button ||
                !label ||
                !(control instanceof HTMLElement) ||
                !(icon instanceof SVGElement)
            ) {
                throw new Error("Missing permanent field measurement target");
            }

            const buttonRect = toBrowserRect(button);
            const controlRect = toBrowserRect(control);
            const iconRect = toBrowserRect(icon);
            const buttonCenter = center(buttonRect);
            const controlCenter = center(controlRect);
            const iconCenter = center(iconRect);

            return {
                button: buttonRect,
                buttonCenterDeltaFromControl: {
                    x: Number((buttonCenter.x - controlCenter.x).toFixed(3)),
                    y: Number((buttonCenter.y - controlCenter.y).toFixed(3)),
                },
                control: controlRect,
                field: toBrowserRect(form),
                icon: iconRect,
                iconCenterDeltaFromButton: {
                    x: Number((iconCenter.x - buttonCenter.x).toFixed(3)),
                    y: Number((iconCenter.y - buttonCenter.y).toFixed(3)),
                },
                input: toBrowserRect(input),
                key: input.dataset.permanentFilterInput ?? "",
                label: label.textContent?.trim() ?? "",
            };
        });
        const rowTops = [
            ...new Set(fields.map((field) => Math.round(field.field.top))),
        ].sort((left, right) => left - right);
        const rows = rowTops.map((top) => ({
            fieldCount: fields.filter(
                (field) => Math.round(field.field.top) === top,
            ).length,
            top,
        }));
        const gridStyles = window.getComputedStyle(searchGrid);
        const columnGap = Number.parseFloat(gridStyles.columnGap) || 0;

        return {
            compactColumnWidth:
                (searchGrid.getBoundingClientRect().width - columnGap * 4) / 5,
            columnGap,
            fieldCount: fields.length,
            fields,
            grid: toBrowserRect(searchGrid),
            rowCount: rowTops.length,
            rows,
            viewport: {
                height: window.innerHeight,
                width: window.innerWidth,
            },
        };
    });
}

test("keeps seeded dashboard permanent search fields compact at narrow tablet widths", async ({
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

        expect.soft(metric.fieldCount).toBe(5);
        expect.soft(metric.compactColumnWidth).toBeGreaterThanOrEqual(112);
        expect.soft(metric.rowCount).toBe(1);
        expect.soft(metric.rows.map((row) => row.fieldCount)).toEqual([5]);

        for (const field of metric.fields) {
            expect
                .soft(Math.abs(field.iconCenterDeltaFromButton.x))
                .toBeLessThanOrEqual(0.25);
            expect
                .soft(Math.abs(field.iconCenterDeltaFromButton.y))
                .toBeLessThanOrEqual(0.25);
            expect
                .soft(Math.abs(field.buttonCenterDeltaFromControl.y))
                .toBeLessThanOrEqual(0.25);
        }
    }

    writeFileSync(
        path.join(evidenceDir, "search-layout-dev-fixtures-postfix.json"),
        `${JSON.stringify({ metrics }, null, 2)}\n`,
    );
});
