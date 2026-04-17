"use client";

import { useTheme } from "next-themes";
import { Toaster as Sonner } from "sonner";

export function Toaster() {
    const { resolvedTheme } = useTheme();

    return (
        <Sonner
            closeButton
            position="top-right"
            richColors
            theme={resolvedTheme === "dark" ? "dark" : "light"}
        />
    );
}
