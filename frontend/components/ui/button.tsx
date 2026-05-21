import type * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
    "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0",
    {
        defaultVariants: {
            size: "default",
            variant: "default",
        },
        variants: {
            size: {
                default: "h-10 px-4 py-2",
                icon: "h-10 w-10",
                sm: "h-9 px-3",
            },
            variant: {
                default:
                    "bg-primary text-primary-foreground shadow-sm hover:bg-primary/90",
                destructive:
                    "bg-destructive text-primary-foreground shadow-sm hover:bg-destructive/90",
                ghost: "text-foreground hover:bg-muted",
                outline:
                    "border border-border bg-background/92 text-foreground shadow-sm hover:bg-muted",
            },
        },
    },
);

function Button({
    asChild = false,
    className,
    size,
    variant,
    ...props
}: React.ComponentProps<"button"> &
    VariantProps<typeof buttonVariants> & {
        asChild?: boolean;
    }) {
    const Comp = asChild ? Slot : "button";

    return (
        <Comp
            data-slot="button"
            className={cn(buttonVariants({ className, size, variant }))}
            {...props}
        />
    );
}

export { Button, buttonVariants };
