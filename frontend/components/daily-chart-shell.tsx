import type { DailyCount } from "@/lib/contracts";

type DailyChartShellProps = {
  data: DailyCount[];
};

export function DailyChartShell({ data }: DailyChartShellProps) {
  const bars = data.length > 0 ? data : Array.from({ length: 30 }, () => null);

  return (
    <section className="overflow-hidden rounded-[1.85rem] border border-border/70 bg-card/85 p-6 shadow-[0_26px_90px_-76px_rgba(46,65,90,0.9)]">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.24em] text-muted-foreground">
            Daily registrations
          </p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight">
            Last 30 days of result activity
          </h2>
        </div>
        <p className="max-w-md text-sm leading-6 text-muted-foreground">
          Daily registrations for the last 30 days, with the newest day
          highlighted.
        </p>
      </div>

      <div className="mt-6 h-[320px] w-full">
        <div
          aria-hidden="true"
          className="grid h-full grid-cols-10 items-end gap-2 rounded-[1.25rem] border border-dashed border-border/70 bg-muted/25 px-3 py-4 sm:grid-cols-15"
        >
          {bars.map((entry, index) => (
            <div
              key={entry?.date ?? `placeholder-${index}`}
              className="rounded-t-full bg-muted-foreground/20"
              data-chart-shell-bar={
                index === bars.length - 1 ? "latest" : "history"
              }
              style={{
                height: `${Math.max(entry?.count ?? index % 4, 1) * 14 + 20}px`,
              }}
            />
          ))}
        </div>
      </div>

      <div className="sr-only" aria-label="Daily registration bars">
        {data.map((entry) => (
          <span key={entry.date} data-daily-bar="true">
            {entry.date}:{entry.count}
          </span>
        ))}
      </div>
    </section>
  );
}
