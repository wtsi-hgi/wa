"use client";

import { TooltipProvider } from "@radix-ui/react-tooltip";
import type { ReactNode } from "react";

import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/toaster";

export function AppProviders({ children }: { children: ReactNode }) {
    return (
        <ThemeProvider
            attribute="class"
            defaultTheme="system"
            enableSystem
            disableTransitionOnChange
        >
            <TooltipProvider delayDuration={120}>
                {children}
                <Toaster />
            </TooltipProvider>
        </ThemeProvider>
    );
}
