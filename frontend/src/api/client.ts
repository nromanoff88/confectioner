import {
  Client,
  ClientDetails,
  ClientInput,
  Note,
  Order,
  OrderInput,
  OrderPhoto,
  OrderStatus,
  Stats,
  TeamInfo,
} from '../types';
import { getMaxInitData } from './bridge';

function authHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra };
  const initData = getMaxInitData();
  if (initData) {
    headers['X-Max-Init-Data'] = initData;
  }
  // Local browser testing with DEV_MODE=true: open the app with ?dev_user=<id>
  const devUser = new URLSearchParams(window.location.search).get('dev_user');
  if (devUser) {
    headers['X-Dev-User-Id'] = devUser;
  }
  return headers;
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  form?: FormData
): Promise<T> {
  const init: RequestInit = { method, headers: authHeaders() };
  if (form) {
    init.body = form;
  } else if (body !== undefined) {
    init.headers = authHeaders({ 'Content-Type': 'application/json' });
    init.body = JSON.stringify(body);
  }
  const res = await fetch(path, init);
  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    try {
      const data = await res.json();
      if (data?.error) message = data.error;
    } catch {
      // ignore
    }
    throw new Error(message);
  }
  return res.json() as Promise<T>;
}

export const api = {
  getHealth: () =>
    request<{ status: string; bot_configured: boolean; bot_name: string; dev_mode: boolean }>(
      'GET',
      '/api/health'
    ),

  getMe: () => request<{ workspace_name: string; role: 'owner' | 'member' }>('GET', '/api/me'),

  listClients: (search = '', tag = '') => {
    const q = new URLSearchParams();
    if (search) q.set('search', search);
    if (tag) q.set('tag', tag);
    return request<Client[]>('GET', `/api/clients?${q.toString()}`);
  },

  createClient: (data: ClientInput) => request<Client>('POST', '/api/clients', data),

  getClientDetails: (id: number) => request<ClientDetails>('GET', `/api/clients/${id}`),

  updateClient: (id: number, data: ClientInput) =>
    request<Client>('PUT', `/api/clients/${id}`, data),

  deleteClient: (id: number) => request<{ deleted: boolean }>('DELETE', `/api/clients/${id}`),

  requestClientMaxLink: (id: number) =>
    request<{ ok: boolean; bot_url: string }>('POST', `/api/clients/${id}/request-link`),

  unlinkClientMax: (id: number) => request<Client>('DELETE', `/api/clients/${id}/link`),

  addNote: (clientId: number, text: string) =>
    request<Note>('POST', `/api/clients/${clientId}/notes`, { text }),

  deleteNote: (noteId: number) => request<{ deleted: boolean }>('DELETE', `/api/notes/${noteId}`),

  saveOrder: (clientId: number, data: OrderInput) =>
    request<Order>('POST', `/api/clients/${clientId}/orders`, data),

  updateOrderStatus: (orderId: number, status: OrderStatus) =>
    request<Order>('POST', `/api/orders/${orderId}/status`, { status }),

  deleteOrder: (orderId: number) =>
    request<{ deleted: boolean }>('DELETE', `/api/orders/${orderId}`),

  listActiveOrders: () => request<Order[]>('GET', '/api/orders/active'),

  uploadOrderPhoto: (orderId: number, kind: 'reference' | 'result', file: File) => {
    const form = new FormData();
    form.append('kind', kind);
    form.append('file', file);
    return request<OrderPhoto>('POST', `/api/orders/${orderId}/photos`, undefined, form);
  },

  deleteOrderPhoto: (orderId: number, photoId: number) =>
    request<{ deleted: boolean }>('DELETE', `/api/orders/${orderId}/photos/${photoId}`),

  getStats: (month: string) => request<Stats>('GET', `/api/stats?month=${encodeURIComponent(month)}`),

  getTeam: () => request<TeamInfo>('GET', '/api/team'),

  removeMember: (userId: number) =>
    request<{ deleted: boolean }>('DELETE', `/api/team/${userId}`),
};
