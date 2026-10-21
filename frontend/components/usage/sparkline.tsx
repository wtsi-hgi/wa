const width = 120;
const height = 28;

// sparklinePoints scales values to the sparkline box: x spread evenly, y from
// 0 at the bottom to the largest value at the top, with a 1px inset so the
// stroke is not clipped.
function sparklinePoints(values: number[]): string {
    const max = Math.max(...values, 1);
    const step = values.length > 1 ? width / (values.length - 1) : 0;

    return values
        .map((value, index) => {
            const x = values.length > 1 ? index * step : width / 2;
            const y = height - 1 - (value / max) * (height - 2);

            return `${x.toFixed(1)},${y.toFixed(1)}`;
        })
        .join(" ");
}

// Sparkline draws values as a small line. It is decorative: the tile states
// the value and change in text. It renders nothing for fewer than two values.
export function Sparkline({
    color = "var(--usage-cli)",
    values,
}: {
    color?: string;
    values: number[];
}) {
    if (values.length < 2) {
        return null;
    }

    return (
        <svg
            aria-hidden="true"
            className="h-7 w-full max-w-32 overflow-visible"
            data-testid="sparkline"
            focusable="false"
            preserveAspectRatio="none"
            viewBox={`0 0 ${width} ${height}`}
        >
            <polyline
                fill="none"
                points={sparklinePoints(values)}
                stroke={color}
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                vectorEffect="non-scaling-stroke"
            />
        </svg>
    );
}
