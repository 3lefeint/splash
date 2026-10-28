import type { PitMenu, Step } from './types';

/** The step that sets the row the game shows right now to its current value. */
export function stepFromRow(row: PitMenu): Step {
  // fuel rows read like "+42/3": litres, then laps
  const litres = /^\+?(\d+)\//.exec(row.choice);
  return { category: row.category, choice: litres ? litres[1] : row.choice };
}

/** Short text for a step, e.g. "FUEL: max". */
export const stepText = (s: Step): string => `${s.category} ${s.choice}`;

/** Example setups offered when a car has none. */
export const EXAMPLES: { name: string; steps: Step[] }[] = [
  { name: 'Max fuel', steps: [{ category: 'FUEL:', choice: 'max' }] },
  { name: 'Medium tyres', steps: [{ category: 'TIRES:', choice: 'medium' }] },
];
