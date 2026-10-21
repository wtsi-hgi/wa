"use client";

import { useEffect } from "react";

// openTarget opens every <details> holding the element the URL hash names,
// and the element itself when it is one, then scrolls to it. It returns
// whether the element exists.
export function openTarget(hash: string): boolean {
    if (hash.length < 2) {
        return false;
    }

    let id: string;

    try {
        id = decodeURIComponent(hash.slice(1));
    } catch {
        // A malformed escape, such as #%E0%A4%A, names no element.
        return false;
    }

    const target = document.getElementById(id);

    if (!target) {
        return false;
    }

    for (
        let node: HTMLElement | null = target;
        node;
        node = node.parentElement
    ) {
        if (node instanceof HTMLDetailsElement) {
            node.open = true;
        }
    }

    target.querySelector(":scope > details")?.setAttribute("open", "");
    target.scrollIntoView?.({ block: "start" });

    return true;
}

// HashOpener makes #err-<id> and section links reach content inside closed
// disclosures, on load and whenever the hash changes.
export function HashOpener() {
    useEffect(() => {
        const open = () => openTarget(window.location.hash);

        open();
        window.addEventListener("hashchange", open);

        return () => window.removeEventListener("hashchange", open);
    }, []);

    return null;
}
