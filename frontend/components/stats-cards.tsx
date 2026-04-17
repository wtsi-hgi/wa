type StatsCardsProps = {
    total: number;
    pipelineCount: number;
    todayCount: number;
};

const cards = [
    {
        key: "total",
        label: "Total result sets",
        valueKey: "total",
        description: "Registered result sets currently tracked by WA.",
    },
    {
        key: "pipelines",
        label: "Distinct pipelines",
        valueKey: "pipelines",
        description: "Unique pipeline names represented in the results store.",
    },
    {
        key: "today",
        label: "Registered today",
        valueKey: "today",
        description: "Registrations recorded in the latest dashboard day bucket.",
    },
] as const;

export function StatsCards({
    total,
    pipelineCount,
    todayCount,
}: StatsCardsProps) {
    const values = {
        total,
        pipelines: pipelineCount,
        today: todayCount,
    } as const;

    return (
        <section className="grid gap-4 md:grid-cols-3">
            {cards.map((card) => (
                <article
                    key={card.key}
                    className="relative overflow-hidden rounded-[1.6rem] border border-border/70 bg-card/85 p-6 shadow-[0_26px_90px_-76px_rgba(46,65,90,0.9)]"
                >
                    <div className="absolute inset-x-6 top-0 h-px bg-gradient-to-r from-transparent via-accent/80 to-transparent" />
                    <p className="text-xs font-semibold uppercase tracking-[0.24em] text-muted-foreground">
                        {card.label}
                    </p>
                    <p
                        className="mt-4 text-4xl font-semibold tracking-tight"
                        data-stat-card={card.valueKey}
                    >
                        {values[card.valueKey]}
                    </p>
                    <p className="mt-3 max-w-xs text-sm leading-6 text-muted-foreground">
                        {card.description}
                    </p>
                </article>
            ))}
        </section>
    );
}
