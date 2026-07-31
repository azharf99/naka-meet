import { describe, test, expect, vi, beforeEach } from 'vitest';
import { signup, login, guestJoin, getSession, logout, createRoom, getRoomInfo } from './auth';

describe('Auth & Room API Service Tests', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  test('signup sends POST to /api/v1/auth/signup with name, email, and password', async () => {
    const mockResponse = { status: 'success', token: 'jwt-123', user_id: 'user-uuid', name: 'Alice', role: 'host' };
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => mockResponse } as Response);

    const result = await signup('Alice', 'alice@example.com', 'correct-password-123');
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/auth/signup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name: 'Alice', email: 'alice@example.com', password: 'correct-password-123' }),
    });
    expect(result).toEqual(mockResponse);
  });

  test('signup throws the server error message on failure', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      text: async () => 'An account with this email already exists\n',
    } as Response);

    await expect(signup('Alice', 'alice@example.com', 'correct-password-123')).rejects.toThrow(
      'An account with this email already exists'
    );
  });

  test('login sends POST to /api/v1/auth/login with email and password', async () => {
    const mockResponse = { status: 'success', token: 'jwt-123', user_id: 'user-uuid', name: 'Alice', role: 'host' };
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => mockResponse } as Response);

    const result = await login('alice@example.com', 'correct-password-123');
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ email: 'alice@example.com', password: 'correct-password-123' }),
    });
    expect(result).toEqual(mockResponse);
  });

  test('login throws on invalid credentials', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      text: async () => 'Invalid email or password\n',
    } as Response);

    await expect(login('alice@example.com', 'wrong')).rejects.toThrow('Invalid email or password');
  });

  test('guestJoin sends POST to /api/v1/auth/guest with name, room_slug, and role', async () => {
    const mockResponse = { status: 'success', token: 'jwt-guest', user_id: 'guest-uuid', name: 'Budi', role: 'guest' };
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => mockResponse } as Response);

    const result = await guestJoin('Budi', 'demo-room', 'guest');
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/auth/guest', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name: 'Budi', room_slug: 'demo-room', role: 'guest' }),
    });
    expect(result).toEqual(mockResponse);
  });

  test('guestJoin defaults role to "guest" when omitted', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'success', token: 't', user_id: 'u', name: 'Budi', role: 'guest' }),
    } as Response);

    await guestJoin('Budi', 'demo-room');
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/v1/auth/guest',
      expect.objectContaining({ body: JSON.stringify({ name: 'Budi', room_slug: 'demo-room', role: 'guest' }) })
    );
  });

  test('getSession returns the session when the cookie is valid', async () => {
    const mockResponse = { user_id: 'user-uuid', name: 'Alice', role: 'host' };
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => mockResponse } as Response);

    const result = await getSession();
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/auth/me', { credentials: 'include' });
    expect(result).toEqual(mockResponse);
  });

  test('getSession returns null when not authenticated', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false } as Response);
    const result = await getSession();
    expect(result).toBeNull();
  });

  test('logout posts to /api/v1/auth/logout', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true } as Response);
    await logout();
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'include',
    });
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
