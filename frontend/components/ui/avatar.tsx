import type * as React from "react";

import { cn } from "@/lib/utils";

function Avatar({ className, ...props }: React.ComponentProps<"span">) {
    return (
        <span
            data-slot="avatar"
            className={cn(
                "relative flex h-9 w-9 shrink-0 overflow-hidden rounded-full",
                className,
            )}
            {...props}
        />
    );
}

function AvatarFallback({ className, ...props }: React.ComponentProps<"span">) {
    return (
        <span
            data-slot="avatar-fallback"
            className={cn(
                "flex h-full w-full items-center justify-center rounded-full bg-muted",
                className,
            )}
            {...props}
        />
    );
}

export { Avatar, AvatarFallback };
