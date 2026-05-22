import type * as React from "react";

import { cn } from "@/lib/utils";

function Alert({ className, ...props }: React.ComponentProps<"div">) {
    return (
        <div
            data-slot="alert"
            className={cn(
                "relative w-full rounded-md border border-border px-3 py-2 text-sm",
                className,
            )}
            {...props}
        />
    );
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
    return (
        <div
            data-slot="alert-title"
            className={cn("font-medium", className)}
            {...props}
        />
    );
}

function AlertDescription({
    className,
    ...props
}: React.ComponentProps<"div">) {
    return (
        <div
            data-slot="alert-description"
            className={cn("text-muted-foreground", className)}
            {...props}
        />
    );
}

export { Alert, AlertDescription, AlertTitle };
