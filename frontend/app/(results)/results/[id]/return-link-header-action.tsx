"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import Link from "next/link";

import { ChevronLeft } from "lucide-react";

type ReturnLinkHeaderActionProps = {
    href: string;
    label: string;
};

function returnLink({ href, label }: ReturnLinkHeaderActionProps) {
    return (
        <Link
            aria-label={label}
            className="inline-flex min-h-9 items-center gap-2 rounded-full border border-border/70 bg-background/85 px-3 py-1.5 text-sm font-medium text-muted-foreground transition hover:text-foreground"
            data-return-link="true"
            href={href}
        >
            <ChevronLeft className="h-3.5 w-3.5" aria-hidden="true" />
            <span>{label}</span>
        </Link>
    );
}

export function ReturnLinkHeaderAction(props: ReturnLinkHeaderActionProps) {
    const [target, setTarget] = useState<HTMLElement | null>(null);

    useEffect(() => {
        const frame = window.requestAnimationFrame(() => {
            setTarget(
                document.querySelector<HTMLElement>(
                    '[data-results-header-actions="true"]',
                ),
            );
        });

        return () => {
            window.cancelAnimationFrame(frame);
        };
    }, []);

    const link = returnLink(props);

    return target ? createPortal(link, target) : link;
}
