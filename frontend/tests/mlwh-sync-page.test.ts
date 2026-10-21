/**
 * @vitest-environment jsdom
 */

import { createElement } from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { mlwhSyncStatusSchema, type MLWHSyncStatus } from "@/lib/contracts";

const actionMocks = vi.hoisted(() => ({
    getMLWHSyncStatusAction: vi.fn(),
}));

vi.mock("@/app/(results)/mlwh-sync/actions", () => actionMocks);

function table(
    name: string,
    status: string,
    overrides: Record<string, unknown> = {},
) {
    return {
        table: name,
        high_water: "2026-10-21T09:00:00Z",
        last_run: "2026-10-21T09:30:00Z",
        ever_synced: true,
        last_success_at: "2026-10-21T09:30:00Z",
        last_error: "",
        last_error_at: "",
        blocked_by: [],
        status,
        ...overrides,
    };
}

function syncStatus(
    status: string,
    tables: ReturnType<typeof table>[],
): MLWHSyncStatus {
    return mlwhSyncStatusSchema.parse({
        status,
        stale_after_seconds: 7200,
        tables,
    });
}

async function renderView(freshness: MLWHSyncStatus) {
    const { MLWHSyncAdminView } =
        await import("@/components/mlwh-sync-admin-view");

    return render(createElement(MLWHSyncAdminView, { freshness }));
}

async function renderPage() {
    const { default: MLWHSyncAdminPage } =
        await import("@/app/(results)/mlwh-sync/page");

    return render(await MLWHSyncAdminPage());
}

function rowFor(name: string): HTMLElement {
    const cell = screen.getByRole("rowheader", { name });
    const row = cell.closest("tr");

    if (!row) {
        throw new Error(`no row for ${name}`);
    }

    return row;
}

afterEach(() => {
    cleanup();
    actionMocks.getMLWHSyncStatusAction.mockReset();
});

describe("MLWHSyncAdminView", () => {
    it("raises an alert banner counting failing and stale tables", async () => {
        await renderView(
            syncStatus("failing", [
                table("sample", "ok"),
                table("study", "stale", {
                    last_success_at: "2026-10-21T05:00:00Z",
                }),
                table("iseq_product_metrics", "failing", {
                    last_error:
                        "mlwh: sync iseq_product_metrics: Error 1205: Lock wait timeout exceeded",
                    last_error_at: "2026-10-21T09:31:00Z",
                }),
                table("seq_product_irods_locations", "failing", {
                    last_success_at: "",
                    last_error:
                        "skipped: dependency sync failed: iseq_product_metrics",
                    last_error_at: "2026-10-21T09:31:00Z",
                    blocked_by: ["iseq_product_metrics"],
                }),
            ]),
        );

        const alert = screen.getByRole("alert");

        expect(alert.textContent).toContain("MLWH sync needs attention");
        expect(alert.textContent).toContain("2 failing");
        expect(alert.textContent).toContain("1 stale");
        expect(alert.textContent).toContain("stale after 2 hours");
    });

    it("lists each table's status, last success and an expandable last error", async () => {
        await renderView(
            syncStatus("failing", [
                table("sample", "ok"),
                table("seq_product_irods_locations", "failing", {
                    last_success_at: "",
                    last_error:
                        "skipped: dependency sync failed: iseq_product_metrics",
                    last_error_at: "2026-10-21T09:31:00Z",
                    blocked_by: ["iseq_product_metrics"],
                }),
            ]),
        );

        const ok = within(rowFor("sample"));

        expect(ok.getByText("OK")).toBeTruthy();
        expect(ok.queryByRole("group")).toBeNull();

        const failing = rowFor("seq_product_irods_locations");

        expect(within(failing).getByText("Failing")).toBeTruthy();
        expect(within(failing).getByText("Never")).toBeTruthy();
        expect(failing.textContent).toContain(
            "Blocked by iseq_product_metrics",
        );

        const details = failing.querySelector("details");

        expect(details).not.toBeNull();
        expect(details?.open).toBe(false);
        expect(details?.querySelector("summary")?.textContent).toContain(
            "skipped: dependency sync failed",
        );
        expect(details?.textContent).toContain(
            "skipped: dependency sync failed: iseq_product_metrics",
        );
    });

    it("shows a quiet status banner and no alert when every table is ok", async () => {
        await renderView(
            syncStatus("ok", [table("sample", "ok"), table("study", "ok")]),
        );

        expect(screen.queryByRole("alert")).toBeNull();
        expect(screen.getByRole("status").textContent).toContain(
            "All 2 tables are in sync",
        );
    });

    it("says never synced in the banner when nothing has synced", async () => {
        await renderView(
            syncStatus("never_synced", [
                table("sample", "never_synced", {
                    ever_synced: false,
                    last_success_at: "",
                    last_run: "",
                }),
            ]),
        );

        expect(screen.getByRole("alert").textContent).toContain(
            "1 never synced",
        );
        expect(within(rowFor("sample")).getByText("Never synced")).toBeTruthy();
    });
});

describe("MLWH sync admin page", () => {
    it("renders the view for an ok state", async () => {
        actionMocks.getMLWHSyncStatusAction.mockResolvedValue({
            status: "ok",
            freshness: syncStatus("ok", [table("sample", "ok")]),
        });

        await renderPage();

        expect(
            screen.getByRole("heading", { level: 1, name: "MLWH sync" }),
        ).toBeTruthy();
        expect(rowFor("sample")).toBeTruthy();
    });

    it.each([
        ["unsupported", "Status unavailable: server too old"],
        ["unavailable", "The MLWH server could not be reached."],
        ["unauthenticated", "Log in to view MLWH sync status."],
        ["forbidden", "You do not have access to MLWH sync status."],
    ])("explains the %s state", async (status, message) => {
        actionMocks.getMLWHSyncStatusAction.mockResolvedValue({ status });

        await renderPage();

        expect(screen.getByRole("status").textContent).toContain(message);
        expect(screen.queryByRole("table")).toBeNull();
    });
});
