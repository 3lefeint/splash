const pad = (n: number) => String(n).padStart(2, '0');

/** 3725 -> "1:02:05", 125 -> "2:05". Negative values get a leading minus. */
export function duration(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return '–';
  const neg = seconds < 0;
  const s = Math.round(Math.abs(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const body = h > 0 ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`;
  return neg ? `−${body}` : body;
}

/** A number with fixed decimals; null and NaN show a dash. */
export function fixed(v: number | null | undefined, digits = 1): string {
  return v == null || !Number.isFinite(v) ? '–' : v.toFixed(digits);
}

/** "+1.2" / "−0.4" with an explicit sign. */
export function signed(v: number | null | undefined, digits = 1): string {
  if (v == null || !Number.isFinite(v)) return '–';
  const body = Math.abs(v).toFixed(digits);
  return v < 0 ? `−${body}` : v > 0 ? `+${body}` : body;
}

/** Wall clock time (HH:mm) of a game time, given the game time of "now". */
export function clockAt(et: number, nowEt: number, nowMs: number): string {
  const d = new Date(nowMs + (et - nowEt) * 1000);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Lap time "1:38.412". */
export function lapTime(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds <= 0) return '–';
  const m = Math.floor(seconds / 60);
  const s = seconds - m * 60;
  return `${m}:${s.toFixed(3).padStart(6, '0')}`;
}
