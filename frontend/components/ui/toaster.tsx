"use client";

import { useSyncExternalStore } from "react";
import { useTheme } from "next-themes";
import { Toaster as Sonner } from "sonner";

export function Toaster() {
    const { resolvedTheme } = useTheme();
    const isMounted = useSyncExternalStore(
        () => () => undefined,
        () => true,
        () => false,
    );

    if (!isMounted) {
        return null;
    }

    return (
        <Sonner
            closeButton
            position="top-right"
            richColors
            theme={resolvedTheme === "dark" ? "dark" : "light"}
        />
    );
}
