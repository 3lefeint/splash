import type { Fit, FitSetting } from './types';

/** The reasons a lap can be left out of the lap time model, in the order they are explained. */
export const EXCLUDE_REASONS = ['manual', 'too_slow', 'out_lap', 'in_lap', 'fcy', 'wet', 'missing_data'] as const;

/** The settings that would change which laps count, given the reasons present: a lower minimum always, and the setting behind each reason that one can change. */
export function fitSettingsFor(fit: Pick<Fit, 'excluded'> | null | undefined): FitSetting[] {
  const reasons = new Set((fit?.excluded ?? []).map((e) => e.reason));
  const out: FitSetting[] = ['fit_min_laps'];
  if (reasons.has('too_slow')) out.push('fit_slow_percent');
  if (reasons.has('fcy')) out.push('fit_skip_fcy');
  if (reasons.has('wet')) out.push('fit_skip_wet');
  return out;
}

/** The excluded laps grouped by reason, laps in order, reasons in the order of EXCLUDE_REASONS; unknown reasons last. */
export function excludedByReason(fit: Pick<Fit, 'excluded'> | null | undefined): { reason: string; laps: number[] }[] {
  const map = new Map<string, number[]>();
  for (const e of fit?.excluded ?? []) map.set(e.reason, [...(map.get(e.reason) ?? []), e.lap]);
  const rank = (r: string) => {
    const i = (EXCLUDE_REASONS as readonly string[]).indexOf(r);
    return i < 0 ? EXCLUDE_REASONS.length : i;
  };
  return [...map.entries()]
    .sort((a, b) => rank(a[0]) - rank(b[0]))
    .map(([reason, laps]) => ({ reason, laps: laps.sort((a, b) => a - b) }));
}
