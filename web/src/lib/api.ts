import type { AdminAgent, AgentInfo, TeamCar, TeamCars, AdminMember, AdminTeam, CreatedAgent, Me, Corner, MapProgress, TrackInfo, SessionSummary, OverrideKey, Params, PitSetup, PlanRow, Step, TrackMap } from './types';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let message = res.statusText;
    try {
      message = ((await res.json()) as { error?: string }).error ?? message;
    } catch {
      // not JSON
    }
    throw new ApiError(message || `HTTP ${res.status}`, res.status);
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

/** The agent the app looks at, or null for the member's own. Requests about one agent carry it. */
let selectedAgent: number | null = null;
export function useAgent(id: number | null): void {
  selectedAgent = id;
}
/** Adds the selected agent to a URL that is about one agent: its state, its overrides, its commands. */
export function withAgent(url: string): string {
  if (selectedAgent === null) return url;
  return url + (url.includes('?') ? '&' : '?') + `agent=${selectedAgent}`;
}

export const getAgents = () => request<AgentInfo[]>('GET', '/api/agents');

export const getPlan = () => request<PlanRow[]>('GET', '/api/stint-plan');
export const putPlan = (rows: PlanRow[]) => request<PlanRow[]>('PUT', '/api/stint-plan', rows);

/** Parameters of a track and car, or null when none are stored yet. */
export async function getParams(track: string, car: string): Promise<Params | null> {
  try {
    return await request<Params>('GET', `/api/params?track=${encodeURIComponent(track)}&car=${encodeURIComponent(car)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export const putParams = (p: Params) =>
  request<Params>('PUT', `/api/params?track=${encodeURIComponent(p.track)}&car=${encodeURIComponent(p.car)}`, p);

export const setOverride = (key: OverrideKey, value: number) =>
  request<{ key: string; value: number }>('PUT', withAgent(`/api/overrides/${key}`), { value });
export const clearOverride = (key: OverrideKey) => request<void>('DELETE', withAgent(`/api/overrides/${key}`));

/** Outline of a track, or null while none has been recorded. */
export async function getTrackMap(track: string): Promise<TrackMap | null> {
  try {
    return await request<TrackMap>('GET', `/api/track-map?track=${encodeURIComponent(track)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export const deleteTrackMap = (track: string) =>
  request<void>('DELETE', `/api/track-map?track=${encodeURIComponent(track)}`);

export const listTracks = () => request<TrackInfo[]>('GET', '/api/tracks');
export const getCorners = (track: string) => request<Corner[]>('GET', `/api/track-corners?track=${encodeURIComponent(track)}`);
export const putCorners = (track: string, corners: Corner[]) =>
  request<Corner[]>('PUT', `/api/track-corners?track=${encodeURIComponent(track)}`, corners);

export const getPitSetups = (car: string) => request<PitSetup[]>('GET', `/api/pit-setups?car=${encodeURIComponent(car)}`);
export const putPitSetup = (s: PitSetup) => request<PitSetup>('PUT', '/api/pit-setups', s);
export const deletePitSetup = (car: string, name: string) =>
  request<void>('DELETE', `/api/pit-setups?car=${encodeURIComponent(car)}&name=${encodeURIComponent(name)}`);
export const applyPitSetup = (name: string, steps: Step[]) =>
  request<{ id: string }>('POST', withAgent('/api/pit-setups/apply'), { name, steps });

/** The outline recorded so far, or null while none is being recorded. */
export async function getMapProgress(track: string): Promise<MapProgress | null> {
  try {
    return await request<MapProgress>('GET', `/api/track-map/progress?track=${encodeURIComponent(track)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export const getSessions = () => request<SessionSummary[]>('GET', '/api/sessions');

/** Address of a CSV export of a session: laps of all cars, pit stops or the conditions of the driven car per lap. */
export const exportUrl = (sessionId: string, kind: 'laps' | 'stops' | 'conditions') =>
  `/api/sessions/${encodeURIComponent(sessionId)}/export?kind=${kind}`;

/** Toggles the pit request of the driven car through the selected agent. */
export const pitRequest = (name: string) =>
  request<{ id: string }>('POST', withAgent(`/api/pit-request?name=${encodeURIComponent(name)}`));

export const getMe = () => request<Me>('GET', '/api/me');

export const adminTeams = () => request<AdminTeam[]>('GET', '/api/admin/teams');
export const adminCreateTeam = (name: string) => request<AdminTeam>('POST', '/api/admin/teams', { name });
export const adminMembers = () => request<AdminMember[]>('GET', '/api/admin/members');
export const adminSetMember = (m: { email: string; team_id: number; is_admin: boolean }) =>
  request<AdminMember>('PUT', '/api/admin/members', m);
export const adminDeleteMember = (email: string) =>
  request<void>('DELETE', `/api/admin/members?email=${encodeURIComponent(email)}`);
export const adminAgents = () => request<AdminAgent[]>('GET', '/api/admin/agents');
export const adminCreateAgent = (name: string, teamId: number, email: string) =>
  request<CreatedAgent>('POST', '/api/admin/agents', { name, team_id: teamId, email });
/** Ties an agent to a member of its team; an empty address releases it. */
export const adminSetAgentEmail = (id: number, email: string) =>
  request<AdminAgent>('PUT', `/api/admin/agents/${id}`, { email });
export const adminRevokeAgent = (id: number) => request<void>('POST', `/api/admin/agents/${id}/revoke`);

export const adminCars = (teamId: number) => request<TeamCars>('GET', `/api/admin/cars?team_id=${teamId}`);
export const adminCreateCar = (teamId: number, name: string) =>
  request<TeamCar>('POST', '/api/admin/cars', { team_id: teamId, name });
export const adminDeleteCar = (teamId: number, id: number) =>
  request<void>('DELETE', `/api/admin/cars/${id}?team_id=${teamId}`);
export const adminMapCarName = (teamId: number, id: number, vehicleName: string) =>
  request<void>('PUT', `/api/admin/cars/${id}/names`, { team_id: teamId, vehicle_name: vehicleName });
export const adminUnmapCarName = (teamId: number, vehicleName: string) =>
  request<void>('DELETE', `/api/admin/car-names?team_id=${teamId}&name=${encodeURIComponent(vehicleName)}`);
