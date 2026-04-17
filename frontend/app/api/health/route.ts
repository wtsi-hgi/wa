import { NextResponse } from "next/server";

import { resultsJson } from "@/lib/backend-client";
import { statsResultSchema } from "@/lib/contracts";

export const dynamic = "force-dynamic";

export async function GET(): Promise<NextResponse> {
    try {
        await resultsJson("/results/stats", statsResultSchema);

        return NextResponse.json({ status: "healthy" });
    } catch {
        return NextResponse.json({ status: "unhealthy" }, { status: 503 });
    }
}
