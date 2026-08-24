export interface SessionResponse {
  status?: string;
  token: string;
  user_id: string;
  name: string;
  role: string;
}

export interface RoomResponse {
  status: string;
  room: {
    id: string;
    slug: string;
    host_id: string;
  };
}

export interface RoomInfoResponse {
  slug: string;
  participant_count: number;
}

export interface IceServer {
  urls: string[];
  username?: string;
  credential?: string;
}

export interface IceServersResponse {
  iceServers: IceServer[];
}

async function parseErrorMessage(res: Response, fallback: string): Promise<string> {
  const text = await res.text().catch(() => '');
  return text.trim() || fallback;
}

// Real host account creation — email + password, persisted server-side.
export async function signup(name: string, email: string, password: string): Promise<SessionResponse> {
  const res = await fetch('/api/v1/auth/signup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name, email, password }),
  });
  if (!res.ok) {
    throw new Error(await parseErrorMessage(res, 'Signup request failed'));
  }
  return res.json();
}

// Real host login — email + password checked against the server's stored
// account. This is the only path that can ever yield a "host" session.
export async function login(email: string, password: string): Promise<SessionResponse> {
  const res = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) {
    throw new Error(await parseErrorMessage(res, 'Login request failed'));
  }
  return res.json();
}

// Ephemeral, no-password join for guests and the egress recorder bot. The
// server ignores/overrides any role other than "guest"/"egress" — a "host"
// session can only come from signup/login above.
export async function guestJoin(name: string, roomSlug: string, role: string = 'guest'): Promise<SessionResponse> {
  const res = await fetch('/api/v1/auth/guest', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name, room_slug: roomSlug, role }),
  });
  if (!res.ok) {
    throw new Error(await parseErrorMessage(res, 'Join request failed'));
  }
  return res.json();
}

// Restores a host session from the httpOnly jwt_token cookie after a page
// reload, so a logged-in host doesn't have to re-enter credentials.
export async function getSession(): Promise<SessionResponse | null> {
  const res = await fetch('/api/v1/auth/me', { credentials: 'include' });
  if (!res.ok) return null;
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'include' });
}

export async function createRoom(slug: string, token: string): Promise<RoomResponse> {
  const res = await fetch('/api/v1/rooms', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    credentials: 'include',
    body: JSON.stringify({ slug }),
  });
  if (!res.ok) {
    throw new Error(await parseErrorMessage(res, 'Create room request failed'));
  }
  return res.json();
}

export async function getRoomInfo(slug: string): Promise<RoomInfoResponse> {
  const res = await fetch(`/api/v1/rooms/${encodeURIComponent(slug)}`);
  if (!res.ok) {
    throw new Error('Room not found');
  }
  return res.json();
}

// Returns this deployment's STUN (and, if the operator has configured
// coturn — see .env.example — time-limited TURN) server list. STUN alone
// silently strands any participant whose network needs a relay (symmetric
// NAT, most mobile carriers, restrictive corporate firewalls); invisible
// on a single LAN, which is exactly why this class of failure is easy to
// ship without noticing.
export async function getIceServers(token: string): Promise<IceServersResponse> {
  const res = await fetch('/api/v1/ice-servers', {
    headers: { Authorization: `Bearer ${token}` },
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error('Failed to fetch ICE server configuration');
  }
  return res.json();
}
