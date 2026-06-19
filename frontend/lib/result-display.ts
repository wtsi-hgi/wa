import type { ResultSet } from "@/lib/contracts";

export function resultProjectName(result: ResultSet): string {
    const project = result.metadata?.project?.trim();

    return project || result.pipeline_name;
}
