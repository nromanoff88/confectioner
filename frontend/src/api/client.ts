import { Client, Message, Order, OrderPhoto, OrderStatus } from '../types';
import { getMaxInitData } from './bridge';

function authHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra };
  const initData = getMaxInitData();
  if (initData) {
    headers['X-Max-Init-Data'] = initData;
  }
  return headers;
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let errText = `HTTP ${res.status}`;
    try {
      const data = await res.json();
      if (data?.error) errText = data.error;
    } catch {
      // ignore
    }
    throw new Error(errText);
  }
  return res.json() as Promise<T>;
}

export const api = {
  async getHealth(): Promise<{
    status: string;
    bot_configured: boolean;
    bot_name: string;
    dev_mode: boolean;
  }> {
    const res = await fetch('/api/health');
    return handleResponse(res);
  },

  async listClients(search = '', filter = 'all'): Promise<Client[]> {
    const q = new URLSearchParams();
    if (search) q.set('search', search);
    if (filter && filter !== 'all') q.set('filter', filter);
    const res = await fetch(`/api/clients?${q.toString()}`, {
      headers: authHeaders(),
    });
    return handleResponse(res);
  },

  async createClient(payload: {
    custom_name: string;
    phone: string;
    address: string;
    description: string;
  }): Promise<Client> {
    const res = await fetch('/api/clients', {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(payload),
    });
    return handleResponse(res);
  },

  async getClientDetails(clientId: number): Promise<{
    client: Client;
    active_order: Order | null;
    history: Order[];
  }> {
    const res = await fetch(`/api/clients/${clientId}`, {
      headers: authHeaders(),
    });
    return handleResponse(res);
  },

  async updateClientProfile(
    clientId: number,
    payload: {
      custom_name: string;
      phone: string;
      address: string;
      description: string;
    }
  ): Promise<Client> {
    const res = await fetch(`/api/clients/${clientId}`, {
      method: 'PUT',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(payload),
    });
    return handleResponse(res);
  },

  async listMessages(clientId: number): Promise<Message[]> {
    const res = await fetch(`/api/clients/${clientId}/messages`, {
      headers: authHeaders(),
    });
    return handleResponse(res);
  },

  async sendMessage(clientId: number, text: string, file?: File | null): Promise<Message> {
    if (file) {
      const form = new FormData();
      form.append('text', text);
      form.append('file', file);
      const res = await fetch(`/api/clients/${clientId}/messages`, {
        method: 'POST',
        headers: authHeaders(),
        body: form,
      });
      return handleResponse(res);
    }

    const res = await fetch(`/api/clients/${clientId}/messages`, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ text }),
    });
    return handleResponse(res);
  },

  async simulateClientMessage(clientId: number, text: string): Promise<Message> {
    const res = await fetch(`/api/clients/${clientId}/simulate`, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ text }),
    });
    return handleResponse(res);
  },

  async saveOrder(
    clientId: number,
    payload: {
      id?: number;
      status: OrderStatus;
      cake_description: string;
      filling: string;
      decor_inscription: string;
      weight_kg: number;
      price: number;
      prepayment: number;
      payment_status: string;
      delivery_type: string;
      delivery_address: string;
      delivery_date: string;
    }
  ): Promise<Order> {
    const res = await fetch(`/api/clients/${clientId}/orders`, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(payload),
    });
    return handleResponse(res);
  },

  async updateOrderStatus(orderId: number, status: OrderStatus): Promise<Order> {
    const res = await fetch(`/api/orders/${orderId}/status`, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ status }),
    });
    return handleResponse(res);
  },

  async uploadOrderPhoto(
    orderId: number,
    kind: 'reference' | 'result',
    file: File,
    caption = ''
  ): Promise<OrderPhoto> {
    const form = new FormData();
    form.append('kind', kind);
    form.append('caption', caption);
    form.append('file', file);
    const res = await fetch(`/api/orders/${orderId}/photos`, {
      method: 'POST',
      headers: authHeaders(),
      body: form,
    });
    return handleResponse(res);
  },

  async attachChatPhotoToOrder(
    orderId: number,
    kind: 'reference' | 'result',
    s3Key: string,
    url: string,
    caption = ''
  ): Promise<OrderPhoto> {
    const res = await fetch(`/api/orders/${orderId}/photos`, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({
        kind,
        s3_key: s3Key,
        url,
        caption,
      }),
    });
    return handleResponse(res);
  },

  async deleteOrderPhoto(orderId: number, photoId: number): Promise<void> {
    const res = await fetch(`/api/orders/${orderId}/photos/${photoId}`, {
      method: 'DELETE',
      headers: authHeaders(),
    });
    await handleResponse(res);
  },
};

