"use client";

import {
    createContext,
    type HTMLAttributes,
    type PropsWithChildren,
    type ReactNode,
    useContext,
    useEffect,
    useId,
    useMemo,
    useRef,
    useState,
} from "react";

import { cn } from "@/lib/utils";

type DropdownMenuContextValue = {
    contentId: string;
    open: boolean;
    setOpen: (open: boolean) => void;
};

const DropdownMenuContext = createContext<DropdownMenuContextValue | null>(
    null,
);

function useDropdownMenuContext(): DropdownMenuContextValue {
    const context = useContext(DropdownMenuContext);

    if (!context) {
        throw new Error(
            "DropdownMenu components must be used within DropdownMenu",
        );
    }

    return context;
}

export function DropdownMenu({ children }: PropsWithChildren): ReactNode {
    const contentId = useId();
    const [open, setOpen] = useState(false);
    const rootRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
        function handlePointerDown(event: MouseEvent) {
            if (!rootRef.current?.contains(event.target as Node)) {
                setOpen(false);
            }
        }

        document.addEventListener("mousedown", handlePointerDown);

        return () => {
            document.removeEventListener("mousedown", handlePointerDown);
        };
    }, []);

    const value = useMemo(
        () => ({
            contentId,
            open,
            setOpen,
        }),
        [contentId, open],
    );

    return (
        <DropdownMenuContext.Provider value={value}>
            <div ref={rootRef} className="relative inline-flex">
                {children}
            </div>
        </DropdownMenuContext.Provider>
    );
}

type DropdownMenuTriggerProps = PropsWithChildren<
    Omit<HTMLAttributes<HTMLButtonElement>, "children">
>;

export function DropdownMenuTrigger({
    children,
    className,
    ...props
}: DropdownMenuTriggerProps): ReactNode {
    const { contentId, open, setOpen } = useDropdownMenuContext();

    return (
        <button
            type="button"
            aria-controls={contentId}
            aria-expanded={open}
            aria-haspopup="menu"
            className={className}
            onClick={() => setOpen(!open)}
            {...props}
        >
            {children}
        </button>
    );
}

type DropdownMenuContentProps = PropsWithChildren<
    Omit<HTMLAttributes<HTMLDivElement>, "children">
>;

export function DropdownMenuContent({
    children,
    className,
    ...props
}: DropdownMenuContentProps): ReactNode {
    const { contentId, open } = useDropdownMenuContext();

    if (!open) {
        return null;
    }

    return (
        <div
            id={contentId}
            role="menu"
            className={cn(
                "absolute right-0 top-[calc(100%+0.5rem)] z-20 min-w-56 rounded-2xl border border-border/80 bg-popover/95 p-2 text-popover-foreground shadow-[0_20px_60px_-28px_rgba(41,58,85,0.55)] backdrop-blur",
                className,
            )}
            {...props}
        >
            {children}
        </div>
    );
}

type DropdownMenuCheckboxItemProps = {
    checked: boolean;
    children: ReactNode;
    className?: string;
    onCheckedChange: (checked: boolean) => void;
} & Omit<HTMLAttributes<HTMLButtonElement>, "children" | "onChange">;

export function DropdownMenuCheckboxItem({
    checked,
    children,
    className,
    onCheckedChange,
    ...props
}: DropdownMenuCheckboxItemProps): ReactNode {
    return (
        <button
            type="button"
            role="menuitemcheckbox"
            aria-checked={checked}
            className={cn(
                "flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left text-sm text-foreground transition hover:bg-muted/70",
                className,
            )}
            onClick={() => onCheckedChange(!checked)}
            {...props}
        >
            <span
                aria-hidden="true"
                className={cn(
                    "flex h-4 w-4 items-center justify-center rounded-[0.35rem] border border-border bg-background text-[10px] font-semibold leading-none",
                    checked
                        ? "border-primary bg-primary text-primary-foreground"
                        : "text-transparent",
                )}
            >
                ✓
            </span>
            <span>{children}</span>
        </button>
    );
}
