import type { SessionSummary } from './types';

export type DriverKind = 'ai' | 'player' | 'mixed' | 'unknown';

/** Who drove the laps with conditions: the AI, a person, both, or unknown when there are none. */
export function driverKind(s: Pick<SessionSummary, 'ai_laps' | 'player_laps'>): DriverKind {
  if (s.ai_laps > 0 && s.player_laps > 0) return 'mixed';
  if (s.ai_laps > 0) return 'ai';
  if (s.player_laps > 0) return 'player';
  return 'unknown';
}
