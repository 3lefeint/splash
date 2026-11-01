import { describe, expect, it } from 'vitest';
import { nextFlag } from './laps';

describe('nextFlag', () => {
  it('leaves a counting lap out of a session that counts with an own flag', () => {
    expect(nextFlag(false, false)).toBe(true);
  });
  it('takes a left-out lap in again by dropping the own flag when the session counts', () => {
    expect(nextFlag(true, false)).toBeNull();
  });
  it('takes a lap in explicitly when its session is left out', () => {
    expect(nextFlag(true, true)).toBe(false);
  });
  it('lets a lap that was taken in follow its left-out session again', () => {
    expect(nextFlag(false, true)).toBeNull();
  });
});
