import { describe, test, expect } from 'vitest';
import { validateJoinInput } from './Lobby';

describe('Lobby Validation Logic Tests', () => {

  test('validateJoinInput requires non-empty display name and room slug', () => {
    expect(validateJoinInput('', 'demo-room').valid).toBe(false);
    expect(validateJoinInput('Budi', '').valid).toBe(false);
    expect(validateJoinInput('Budi', 'demo-room').valid).toBe(true);
  });
});
