export type ResultRegistrationField = {
    label: string;
    mono?: boolean;
    value: string;
    wide?: boolean;
};

type ResultRegistrationSummaryProps = {
    fields: ResultRegistrationField[];
};

export function ResultRegistrationSummary({
    fields,
}: ResultRegistrationSummaryProps) {
    const compactFields = fields.filter((field) => !field.wide);
    const wideFields = fields.filter((field) => field.wide);

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
