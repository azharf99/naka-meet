import { describe, test, expect, vi, beforeEach } from 'vitest';
import { loginUser, createRoom, getRoomInfo } from './auth';

describe('Auth & Room API Service Tests', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  test('loginUser sends POST to /api/v1/auth/login with name and role', async () => {
    const mockResponse = {
      status: 'success',
      token: 'jwt-123',
      user_id: 'user-uuid',
      name: 'Budi',
      role: 'guest',
    };

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response);

    const result = await loginUser('Budi', 'guest');
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name: 'Budi', role: 'guest' }),
    });
    expect(result).toEqual(mockResponse);
  });

  test('createRoom sends POST to /api/v1/rooms with slug and bearer token', async () => {
    const mockResponse = {
      status: 'success',
      room: { id: 'room-uuid', slug: 'my-meeting', host_id: 'host-uuid' },
    };

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response);

    const result = await createRoom('my-meeting', 'jwt-host-token');
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/rooms', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer jwt-host-token',
      },
      credentials: 'include',
      body: JSON.stringify({ slug: 'my-meeting' }),
    });
    expect(result).toEqual(mockResponse);
  });

  test('getRoomInfo sends GET to /api/v1/rooms/:slug', async () => {
    const mockResponse = {
      slug: 'my-meeting',
      participant_count: 3,
    };

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => mockResponse,
    } as Response);

    const result = await getRoomInfo('my-meeting');
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/rooms/my-meeting');
    expect(result).toEqual(mockResponse);
  });
});
