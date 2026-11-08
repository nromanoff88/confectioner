export interface Attachment {
  type: string;
  s3_key: string;
  url: string;
  filename?: string;
}

export interface ActiveOrderBrief {
  id: number;
  status: OrderStatus;
  cake_description: string;
  weight_kg: number;
  price: number;
  delivery_date?: string;
}

export interface Client {
  id: number;
  max_user_id: number;
  max_chat_id: number;
  first_name: string;
  last_name: string;
  username: string;
  avatar_url: string;
  custom_name: string;
  phone: string;
  address: string;
  description: string;
  unread_count: number;
  last_message_text: string;
  last_message_at: string;
  created_at: string;
  updated_at: string;
  active_order?: ActiveOrderBrief;
}

export interface Message {
  id: number;
  client_id: number;
  max_mid: string;
  sender_type: 'client' | 'operator' | 'system';
  text: string;
  attachments: Attachment[];
  created_at: string;
}

export type OrderStatus =
  | 'new'
  | 'awaiting_prepayment'
  | 'in_progress'
  | 'ready'
  | 'completed'
  | 'cancelled';

export type PaymentStatus = 'unpaid' | 'prepaid' | 'paid';
export type DeliveryType = 'pickup' | 'delivery';

export interface OrderPhoto {
  id: number;
  order_id: number;
  kind: 'reference' | 'result';
  s3_key: string;
  url: string;
  caption: string;
  created_at: string;
}

export interface Order {
  id: number;
  client_id: number;
  status: OrderStatus;
  cake_description: string;
  filling: string;
  decor_inscription: string;
  weight_kg: number;
  price: number;
  prepayment: number;
  payment_status: PaymentStatus;
  delivery_type: DeliveryType;
  delivery_address: string;
  delivery_date?: string | null;
  photos: OrderPhoto[];
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
}

export const ORDER_STATUS_LABELS: Record<OrderStatus, string> = {
  new: '📝 Новый / Обсуждение',
  awaiting_prepayment: '⏳ Ожидает предоплаты',
  in_progress: '👩‍🍳 В работе',
  ready: '🎂 Готов к выдаче',
  completed: '✅ Выполнен (в архив)',
  cancelled: '❌ Отменён',
};

export const PAYMENT_STATUS_LABELS: Record<PaymentStatus, string> = {
  unpaid: 'Не оплачен',
  prepaid: 'Внесена предоплата',
  paid: 'Оплачен полностью',
};

export function clientDisplayName(c: Client): string {
  if (c.custom_name && c.custom_name.trim()) return c.custom_name.trim();
  const full = `${c.first_name || ''} ${c.last_name || ''}`.trim();
  if (full) return full;
  if (c.username) return `@${c.username}`;
  return `Клиент #${c.id}`;
}

