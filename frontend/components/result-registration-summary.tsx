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

export function ResultRegistrationSummary({
    fields,
    variant = "section",
}: ResultRegistrationSummaryProps) {
    const compactFields = fields.filter((field) => !field.wide);
    const wideFields = fields.filter((field) => field.wide);

    if (variant === "integrated") {
        return (
            <div className="space-y-3" data-registration-summary="integrated">
                <div className="flex items-center justify-between gap-3">
                    <p className="text-sm font-semibold text-foreground">
                        Registration details
                    </p>
                    <p className="text-xs text-muted-foreground">
                        {fields.length} fields
                    </p>
                </div>

                <div className="grid gap-2">
                    <dl
                        className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3"
                        data-registration-layout="integrated"
                    >
                        {compactFields.map((field) => (
                            <div
                                key={field.label}
                                className="min-w-0 rounded-lg border border-border/60 bg-background/65 px-3 py-2"
                                data-registration-field={field.label}
                            >
                                <dt className="text-xs font-medium text-muted-foreground">
                                    {field.label}
                                </dt>
                                <dd
                                    className={
                                        field.mono
                                            ? "mt-1 break-all font-mono text-xs text-foreground"
                                            : "mt-1 break-words text-sm leading-5 text-foreground"
                                    }
                                >
                                    {field.value}
                                </dd>
                            </div>
                        ))}
                    </dl>

                    <dl className="grid gap-2 lg:grid-cols-2">
                        {wideFields.map((field) => (
                            <div
                                key={field.label}
                                className="min-w-0 rounded-lg border border-border/60 bg-background/65 px-3 py-2"
                                data-registration-wide-field={field.label}
                            >
                                <dt className="text-xs font-medium text-muted-foreground">
                                    {field.label}
                                </dt>
                                <dd
                                    className={
                                        field.mono
                                            ? "mt-1 break-all font-mono text-xs text-foreground"
                                            : "mt-1 text-sm leading-6 text-foreground"
                                    }
                                >
                                    {field.value}
                                </dd>
                            </div>
                        ))}
                    </dl>
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
