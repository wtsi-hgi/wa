"use client";

import {
    createContext,
    useContext,
    useId,
    useMemo,
    useState,
    type ComponentPropsWithoutRef,
    type ReactNode,
} from "react";

import { cn } from "@/lib/utils";

type TabsContextValue = {
    baseId: string;
    setValue: (value: string) => void;
    value: string;
};

const TabsContext = createContext<TabsContextValue | null>(null);

type TabsProps = ComponentPropsWithoutRef<"div"> & {
    children: ReactNode;
    defaultValue?: string;
    onValueChange?: (value: string) => void;
    value?: string;
};

function useTabsContext() {
    const context = useContext(TabsContext);

    if (!context) {
        throw new Error("Tabs components must be rendered within <Tabs>");
    }

    return context;
}

export function Tabs({
    children,
    className,
    defaultValue,
    onValueChange,
    value,
    ...props
}: TabsProps) {
    const generatedId = useId();
    const [internalValue, setInternalValue] = useState(defaultValue ?? "");
    const currentValue = value ?? internalValue;

    const context = useMemo<TabsContextValue>(
        () => ({
            baseId: generatedId,
            setValue: (nextValue: string) => {
                if (value === undefined) {
                    setInternalValue(nextValue);
                }

                onValueChange?.(nextValue);
            },
            value: currentValue,
        }),
        [currentValue, generatedId, onValueChange, value],
    );

    return (
        <TabsContext.Provider value={context}>
            <div className={cn("flex flex-col gap-4", className)} {...props}>
                {children}
            </div>
        </TabsContext.Provider>
    );
}

export function TabsList({
    className,
    ...props
}: ComponentPropsWithoutRef<"div">) {
    return (
        <div
            role="tablist"
            className={cn(
                "inline-flex w-full flex-wrap gap-2 rounded-2xl border border-border/70 bg-muted/40 p-1.5",
                className,
            )}
            {...props}
        />
    );
}

type TabsTriggerProps = ComponentPropsWithoutRef<"button"> & {
    value: string;
};

export function TabsTrigger({ className, value, ...props }: TabsTriggerProps) {
    const { baseId, setValue, value: selectedValue } = useTabsContext();
    const isActive = selectedValue === value;

    return (
        <button
            type="button"
            aria-controls={`${baseId}-content-${value}`}
            aria-selected={isActive}
            className={cn(
                "inline-flex min-w-[7rem] items-center justify-center rounded-[1rem] px-4 py-2.5 text-sm font-medium transition",
                isActive
                    ? "bg-background text-foreground shadow-[0_10px_30px_-24px_rgba(32,48,76,0.9)]"
                    : "text-muted-foreground hover:bg-background/70 hover:text-foreground",
                className,
            )}
            data-state={isActive ? "active" : "inactive"}
            id={`${baseId}-trigger-${value}`}
            role="tab"
            tabIndex={isActive ? 0 : -1}
            value={value}
            onClick={() => setValue(value)}
            {...props}
        />
    );
}

type TabsContentProps = ComponentPropsWithoutRef<"div"> & {
    value: string;
};

export function TabsContent({
    children,
    className,
    value,
    ...props
}: TabsContentProps) {
    const { baseId, value: selectedValue } = useTabsContext();
    const isActive = selectedValue === value;

    return (
        <div
            aria-labelledby={`${baseId}-trigger-${value}`}
            className={cn("outline-none", className)}
            hidden={!isActive}
            id={`${baseId}-content-${value}`}
            role="tabpanel"
            {...props}
        >
            {children}
        </div>
    );
}
