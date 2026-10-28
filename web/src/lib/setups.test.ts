import { describe, expect, it } from 'vitest';
import { stepFromRow, stepText } from './setups';

describe('setups', () => {
  it('takes litres from a fuel row', () => {
    const row = { category: 'FUEL:', choice: '+42/3', choice_index: 42, choices: 123 };
    expect(stepFromRow(row)).toEqual({ category: 'FUEL:', choice: '42' });
  });

  it('keeps the text of other rows', () => {
    const row = { category: 'TIRES:', choice: 'Medium', choice_index: 1, choices: 3 };
    expect(stepFromRow(row)).toEqual({ category: 'TIRES:', choice: 'Medium' });
    expect(stepText({ category: 'TIRES:', choice: 'Medium' })).toBe('TIRES: Medium');
  });
});
