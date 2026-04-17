"use client";

import { useEffect } from "react";
import { toast } from "sonner";

export function DashboardToast({ message }: { message: string | null }) {
    useEffect(() => {
        if (message) {
            toast.error(message);
        }
    }, [message]);

    if (!message) {
        return null;
    }

    return <span className="sr-only" data-toast-message={message} />;
}
