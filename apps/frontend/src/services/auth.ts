export interface LoginResponse {
  status: string;
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

export async function loginUser(name: string, role: string): Promise<LoginResponse> {
  const res = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name, role }),
  });
  if (!res.ok) {
    throw new Error('Login request failed');
  }
  return res.json();
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
    throw new Error('Create room request failed');
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
