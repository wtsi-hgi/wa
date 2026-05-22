import type { ReactNode } from "react";

import { AuthMenu } from "@/components/auth-menu";
import { SeqmetaCacheProvider } from "@/lib/seqmeta-cache";
import { currentSession } from "@/app/(results)/auth/actions";

export default async function ResultsLayout({
    children,
}: {
    children: ReactNode;
}) {
    const session = await currentSession();

    return (
        <SeqmetaCacheProvider>
            <div className="min-h-screen">
                <header
                    className="mx-auto flex w-full max-w-[84rem] items-start justify-between gap-3 px-4 pt-4 sm:px-8 sm:pt-6"
                    data-results-auth-bar="true"
                >
                    <div
                        className="flex min-h-11 min-w-0 items-center"
                        data-results-header-actions="true"
                    />
                    <div className="flex min-w-0 justify-end">
                        <AuthMenu initialSession={session} />
                    </div>
                </header>
                {children}
            </div>
        </SeqmetaCacheProvider>
    );
}
