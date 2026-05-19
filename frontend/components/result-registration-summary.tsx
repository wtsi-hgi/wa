import { Info } from "lucide-react";

import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover";

export type ResultRegistrationField = {
    label: string;
    mono?: boolean;
    value: string;
    wide?: boolean;
};

type ResultRegistrationSummaryProps = {
    fields: ResultRegistrationField[];
    variant?: "section" | "integrated";
};

const PRIORITY_FIELD_LABELS = ["Last updated", "Requester", "Operator"];

function visibleIntegratedFields(fields: ResultRegistrationField[]) {
    const priorityFields = PRIORITY_FIELD_LABELS.map((label) =>
        fields.find((field) => field.label === label),
    ).filter((field): field is ResultRegistrationField => Boolean(field));

    if (priorityFields.length >= 3) {
        return priorityFields;
    }

    return fields
        .filter((field) => !field.wide)
        .filter((field) => !priorityFields.includes(field))
        .slice(0, 4 - priorityFields.length)
        .reduce(
            (acc, field) => [...acc, field],
            priorityFields as ResultRegistrationField[],
        );
}

export function ResultRegistrationSummary({
    fields,
    variant = "section",
}: ResultRegistrationSummaryProps) {
    const compactFields = fields.filter((field) => !field.wide);
    const wideFields = fields.filter((field) => field.wide);
    const integratedFields = visibleIntegratedFields(fields);

    if (variant === "integrated") {
        return (
            <div className="min-w-0" data-registration-summary="integrated">
                <div
                    className="flex flex-wrap items-center gap-2"
                    data-registration-layout="integrated"
                >
                    <span className="shrink-0 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                        Run details
                    </span>

                    <dl className="contents">
                        {integratedFields.map((field) => (
                            <div
                                key={field.label}
                                className="inline-flex min-h-8 max-w-full items-center gap-2 rounded-full border border-border/65 bg-background/70 px-3 py-1 text-xs shadow-[0_10px_28px_-26px_rgba(28,40,58,0.72)]"
                                data-registration-field={field.label}
                            >
                                <dt className="shrink-0 font-medium text-muted-foreground">
                                    {field.label}
                                </dt>
                                <dd
                                    className={
                                        field.mono
                                            ? "min-w-0 truncate font-mono text-foreground"
                                            : "min-w-0 truncate text-foreground"
                                    }
                                >
                                    {field.value}
                                </dd>
                            </div>
                        ))}
                    </dl>

                    <Popover>
                        <PopoverTrigger
                            className="inline-flex min-h-8 items-center gap-1.5 rounded-full border border-border/70 bg-card/70 px-3 py-1 text-xs font-medium text-muted-foreground transition hover:border-primary/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                            data-registration-details-trigger="true"
                        >
                            <Info className="h-3.5 w-3.5" aria-hidden="true" />
                            <span>All details</span>
                        </PopoverTrigger>
                        <PopoverContent
                            align="start"
                            className="w-[min(92vw,46rem)] p-4"
                        >
                            <div className="flex items-center justify-between gap-3">
                                <p className="text-sm font-semibold text-foreground">
                                    Run details
                                </p>
                                <p className="text-xs text-muted-foreground">
                                    {fields.length} fields
                                </p>
                            </div>

                            <dl
                                className="mt-3 grid max-h-[min(24rem,70vh)] gap-2 overflow-auto pr-1 sm:grid-cols-2"
                                data-registration-details-panel="true"
                            >
                                {fields.map((field) => (
                                    <div
                                        key={field.label}
                                        className={
                                            field.wide
                                                ? "min-w-0 rounded-lg border border-border/60 bg-background/70 px-3 py-2 sm:col-span-2"
                                                : "min-w-0 rounded-lg border border-border/60 bg-background/70 px-3 py-2"
                                        }
                                        data-registration-detail-field={
                                            field.label
                                        }
                                    >
                                        <dt className="text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                                            {field.label}
                                        </dt>
                                        <dd
                                            className={
                                                field.mono
                                                    ? "mt-1 break-all font-mono text-xs leading-5 text-foreground"
                                                    : "mt-1 break-words text-sm leading-5 text-foreground"
                                            }
                                        >
                                            {field.value}
                                        </dd>
                                    </div>
                                ))}
                            </dl>
                        </PopoverContent>
                    </Popover>
                </div>
            </div>
        );
    }

    return (
        <section className="rounded-[1.75rem] border border-border/70 bg-card/85 p-6 shadow-[0_24px_90px_-72px_rgba(48,67,98,0.85)]">
            <div className="space-y-2">
                <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                    Registration
                </p>
                <h2 className="text-2xl font-semibold tracking-tight">
                    Key details
                </h2>
            </div>

            <div className="mt-6 grid gap-5 xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
                <dl
                    className="grid gap-x-5 gap-y-3 sm:grid-cols-2 xl:grid-cols-3"
                    data-registration-layout="compact"
                >
                    {compactFields.map((field) => (
                        <div
                            key={field.label}
                            className="border-b border-border/60 pb-3"
                            data-registration-field={field.label}
                        >
                            <dt className="text-[11px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                {field.label}
                            </dt>
                            <dd
                                className={
                                    field.mono
                                        ? "mt-1 break-all font-mono text-sm text-foreground"
                                        : "mt-1 text-sm leading-6 text-foreground"
                                }
                            >
                                {field.value}
                            </dd>
                        </div>
                    ))}
                </dl>

                <dl className="grid gap-3">
                    {wideFields.map((field) => (
                        <div
                            key={field.label}
                            className="rounded-[1.25rem] border border-border/70 bg-background/60 px-4 py-3"
                            data-registration-wide-field={field.label}
                        >
                            <dt className="text-[11px] font-semibold uppercase tracking-[0.22em] text-muted-foreground">
                                {field.label}
                            </dt>
                            <dd
                                className={
                                    field.mono
                                        ? "mt-2 break-all font-mono text-sm text-foreground"
                                        : "mt-2 text-sm leading-6 text-foreground"
                                }
                            >
                                {field.value}
                            </dd>
                        </div>
                    ))}
                </dl>
            </div>
        </section>
    );
}
