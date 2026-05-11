import type { ReactNode } from "react";

import { SeqmetaCacheProvider } from "@/lib/seqmeta-cache";

export default function ResultsLayout({ children }: { children: ReactNode }) {
    return <SeqmetaCacheProvider>{children}</SeqmetaCacheProvider>;
}
