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
                <div className="fixed top-4 right-4 z-40 sm:top-6 sm:right-6">
                    <AuthMenu initialSession={session} />
                </div>
                {children}
            </div>
        </SeqmetaCacheProvider>
    );
}
