import type * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
    "inline-flex items-center rounded-md border px-2.5 py-0.5 text-xs font-medium whitespace-nowrap transition-colors",
    {
        defaultVariants: {
            variant: "secondary",
        },
        variants: {
            variant: {
                default:
                    "border-transparent bg-primary text-primary-foreground",
                outline: "border-border text-foreground",
                secondary:
                    "border-border bg-secondary text-secondary-foreground",
            },
        },
    },
);

function Badge({
    className,
    variant,
    ...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
    return (
        <span
            data-slot="badge"
            className={cn(badgeVariants({ className, variant }))}
            {...props}
        />
    );
}

export { Badge, badgeVariants };
