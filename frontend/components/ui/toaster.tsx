"use client";

import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { Toaster as Sonner } from "sonner";

export function Toaster() {
    const { resolvedTheme } = useTheme();
    const [isMounted, setIsMounted] = useState(false);

    useEffect(() => {
        setIsMounted(true);
    }, []);

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
