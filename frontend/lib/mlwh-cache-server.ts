import { cookies } from "next/headers";

import {
    deserializeMLWHCacheCookie,
    MLWHCache,
    MLWH_CACHE_COOKIE_NAME,
    type MLWHCacheStore,
} from "@/lib/mlwh-cache-core";

export async function getRequestMLWHCache(): Promise<MLWHCacheStore> {
    const cookieStore = await cookies();

    return new MLWHCache(
        deserializeMLWHCacheCookie(
            cookieStore.get(MLWH_CACHE_COOKIE_NAME)?.value,
        ),
    );
}
