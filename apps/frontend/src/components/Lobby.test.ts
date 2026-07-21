import { describe, test, expect } from 'vitest';
import { validateJoinInput, shouldAutoJoinEgress } from './Lobby';

describe('Lobby Validation Logic Tests', () => {

  test('validateJoinInput requires non-empty display name and room slug', () => {
    expect(validateJoinInput('', 'demo-room').valid).toBe(false);
    expect(validateJoinInput('Budi', '').valid).toBe(false);
    expect(validateJoinInput('Budi', 'demo-room').valid).toBe(true);
  });

  test('shouldAutoJoinEgress returns true when role=egress and room slug is present', () => {
    expect(shouldAutoJoinEgress('egress', 'demo-room')).toBe(true);
    expect(shouldAutoJoinEgress('host', 'demo-room')).toBe(false);
    expect(shouldAutoJoinEgress('egress', '')).toBe(false);
  });
});


