import { cookies } from "next/headers";

import {
    deserializeSeqmetaCacheCookie,
    SeqmetaCache,
    SEQMETA_CACHE_COOKIE_NAME,
    type SeqmetaCacheStore,
} from "@/lib/seqmeta-cache-core";

export async function getRequestSeqmetaCache(): Promise<SeqmetaCacheStore> {
    const cookieStore = await cookies();

    return new SeqmetaCache(
        deserializeSeqmetaCacheCookie(
            cookieStore.get(SEQMETA_CACHE_COOKIE_NAME)?.value,
        ),
    );
}
