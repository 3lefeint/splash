import { describe, expect, it } from 'vitest';
import { driverKind } from './sessions';

describe('driverKind', () => {
  it('tells AI, person, both and none apart', () => {
    expect(driverKind({ ai_laps: 3, player_laps: 0 })).toBe('ai');
    expect(driverKind({ ai_laps: 0, player_laps: 2 })).toBe('player');
    expect(driverKind({ ai_laps: 1, player_laps: 1 })).toBe('mixed');
    expect(driverKind({ ai_laps: 0, player_laps: 0 })).toBe('unknown');
  });
});
