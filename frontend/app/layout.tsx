import type { Metadata } from "next";
import { Inter } from "next/font/google";

import { AppProviders } from "@/components/app-providers";

import "./globals.css";

const inter = Inter({
    subsets: ["latin"],
    display: "swap",
    variable: "--font-inter",
});

export const metadata: Metadata = {
    title: "WA Results",
    description: "Results browser scaffold for the WA results web UI.",
};

export default function RootLayout({
    children,
}: Readonly<{
    children: React.ReactNode;
}>) {
    return (
        <html lang="en" suppressHydrationWarning>
            <body
                className={`${inter.variable} min-h-screen bg-background text-foreground antialiased`}
            >
                <AppProviders>{children}</AppProviders>
            </body>
        </html>
    );
}
