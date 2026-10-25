export type Tone = 'good' | 'bad' | 'neutral';

/** Trends below this many seconds per lap count as steady. */
export const STEADY = 0.05;

/**
 * Colour of a gap trend. A shrinking gap (negative trend) is good for the car ahead
 * of us (we catch it) and bad for the car behind (it catches us).
 */
export function trendTone(side: 'ahead' | 'behind', trend: number | null): Tone {
  if (trend == null || Math.abs(trend) < STEADY) return 'neutral';
  const closing = trend < 0;
  return closing === (side === 'ahead') ? 'good' : 'bad';
}
