export interface ActiveOrderBrief {
  id: number;
  status: OrderStatus;
  cake_description: string;
  weight_kg: number;
  price: number;
  delivery_date?: string | null;
}

export interface Client {
  id: number;
  name: string;
  phone: string;
  address: string;
  description: string;
  max_username: string;
  tags: string[];
  created_at: string;
  updated_at: string;
  active_order?: ActiveOrderBrief;
  orders_count: number;
  total_spent: number;
}

export interface ClientInput {
  name: string;
  phone: string;
  address: string;
  description: string;
  max_username: string;
  tags: string[];
}

export interface Note {
  id: number;
  client_id: number;
  author_name: string;
  text: string;
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
  client_name: string;
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
  remind_before_min: number;
  photos: OrderPhoto[];
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
}

export interface OrderInput {
  id?: number;
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
  delivery_date: string;
  remind_before_min: number;
}

export interface ClientDetails {
  client: Client;
  active_order: Order | null;
  history: Order[];
  notes: Note[];
}

export interface Member {
  max_user_id: number;
  workspace_name: string;
  first_name: string;
  last_name: string;
  username: string;
  role: 'owner' | 'member';
  created_at: string;
}

export interface TeamInfo {
  workspace_name: string;
  is_owner: boolean;
  me: number;
  members: Member[];
  invite_code?: string;
  invite_link?: string;
}

export interface TopClient {
  client_id: number;
  name: string;
  orders: number;
  revenue: number;
}

export interface Stats {
  month: string;
  revenue: number;
  orders_completed: number;
  weight_kg: number;
  avg_check: number;
  new_clients: number;
  active_orders: number;
  outstanding: number;
  top_clients: TopClient[];
}

export const ORDER_STATUS_LABELS: Record<OrderStatus, string> = {
  new: '📝 Новый',
  awaiting_prepayment: '⏳ Ожидает предоплаты',
  in_progress: '👩‍🍳 В работе',
  ready: '🎂 Готов к выдаче',
  completed: '✅ Выполнен',
  cancelled: '❌ Отменён',
};

export const PAYMENT_STATUS_LABELS: Record<PaymentStatus, string> = {
  unpaid: 'Не оплачен',
  prepaid: 'Внесена предоплата',
  paid: 'Оплачен полностью',
};

export const REMIND_OPTIONS: { value: number; label: string }[] = [
  { value: 0, label: 'Не напоминать' },
  { value: 60, label: 'За 1 час' },
  { value: 120, label: 'За 2 часа' },
  { value: 360, label: 'За 6 часов' },
  { value: 1440, label: 'За 1 день' },
  { value: 2880, label: 'За 2 дня' },
];

export const TAG_PRESETS = ['VIP', 'Аллергик', 'Корпоратив', 'Постоянный'];

export function clientDisplayName(c: Pick<Client, 'id' | 'name'>): string {
  return c.name && c.name.trim() ? c.name.trim() : `Клиент #${c.id}`;
}

export function memberDisplayName(m: Member): string {
  const full = `${m.first_name || ''} ${m.last_name || ''}`.trim();
  if (full) return full;
  if (m.username) return `@${m.username}`;
  return `Кондитер #${m.max_user_id}`;
}

export function formatMoney(v: number): string {
  return `${Math.round(v).toLocaleString('ru-RU')} ₽`;
}
