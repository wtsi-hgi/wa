import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Table({ className, ...props }: ComponentProps<"table">) {
    return (
        <div className="relative w-full overflow-x-auto">
            <table
                className={cn("w-full caption-bottom text-sm", className)}
                {...props}
            />
        </div>
    );
}

export function TableHeader({ className, ...props }: ComponentProps<"thead">) {
    return <thead className={cn("[&_tr]:border-b", className)} {...props} />;
}

export function TableBody({ className, ...props }: ComponentProps<"tbody">) {
    return (
        <tbody className={cn("[&_tr:last-child]:border-0", className)} {...props} />
    );
}

export function TableRow({ className, ...props }: ComponentProps<"tr">) {
    return (
        <tr
            className={cn(
                "border-b border-border/60 transition-colors hover:bg-muted/25 data-[state=selected]:bg-muted",
                className,
            )}
            {...props}
        />
    );
}

export function TableHead({ className, ...props }: ComponentProps<"th">) {
    return (
        <th
            className={cn(
                "h-12 px-4 text-left align-middle text-sm font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0",
                className,
            )}
            {...props}
        />
    );
}

export function TableCell({ className, ...props }: ComponentProps<"td">) {
    return (
        <td
            className={cn(
                "px-4 py-3 align-middle [&:has([role=checkbox])]:pr-0",
                className,
            )}
            {...props}
        />
    );
}

export function TableCaption({
    className,
    ...props
}: ComponentProps<"caption">) {
    return (
        <caption
            className={cn("mt-4 text-sm text-muted-foreground", className)}
            {...props}
        />
    );
}
