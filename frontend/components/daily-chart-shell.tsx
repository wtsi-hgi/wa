import type { DailyCount } from "@/lib/contracts";

type DailyChartShellProps = {
  data: DailyCount[];
};

export function DailyChartShell({ data }: DailyChartShellProps) {
  const hasData = data.length > 0;

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
          {hasData
            ? "Daily registrations for the last 30 days, with the newest day highlighted."
            : "No result activity recorded for the last 30 days."}
        </p>
      </div>

      <div className="mt-6 h-[320px] w-full">
        {hasData ? (
          <div
            aria-hidden="true"
            className="grid h-full items-end gap-2 rounded-[1.25rem] border border-dashed border-border/70 bg-muted/25 px-3 py-4"
            style={{
              gridTemplateColumns: `repeat(${data.length}, minmax(0, 1fr))`,
            }}
          >
            {data.map((entry, index) => (
              <div
                key={entry.date}
                className={
                  index === data.length - 1
                    ? "rounded-t-full bg-accent"
                    : "rounded-t-full bg-muted-foreground/20"
                }
                data-chart-shell-bar={
                  index === data.length - 1 ? "latest" : "history"
                }
                style={{
                  height: entry.count > 0 ? `${entry.count * 14 + 20}px` : "0px",
                }}
              />
            ))}
          </div>
        ) : (
          <div className="flex h-full items-center justify-center rounded-[1.25rem] border border-dashed border-border/70 bg-muted/25 px-6 text-center">
            <p className="max-w-sm text-sm leading-6 text-muted-foreground">
              No result activity recorded for the last 30 days.
            </p>
          </div>
        )}
      </div>

      {hasData ? (
        <div className="sr-only" aria-label="Daily registration bars">
          {data.map((entry) => (
            <span key={entry.date} data-daily-bar="true">
              {entry.date}:{entry.count}
            </span>
          ))}
        </div>
      ) : null}
    </section>
  );
}
