import {
    ResultDetailPageContent,
    type ResultDetailPageProps,
    resolveResultDetailPageProps,
} from "@/app/(results)/results/[id]/page-content";

export const dynamic = "force-dynamic";

export default async function ResultDetailPage(props: ResultDetailPageProps) {
    const resolvedProps = await resolveResultDetailPageProps(props);

    return <ResultDetailPageContent {...resolvedProps} />;
}
