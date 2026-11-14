import React from 'react';
import { formatMoney, Order, ORDER_STATUS_LABELS } from '../types';

interface CalendarViewProps {
  orders: Order[];
  loading: boolean;
  onOpenClient: (clientId: number) => void;
}

interface Group {
  key: string;
  title: string;
  tone: 'late' | 'today' | 'normal' | 'none';
  orders: Order[];
}

function dayStart(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

function buildGroups(orders: Order[]): Group[] {
  const now = new Date();
  const today = dayStart(now);
  const day = 24 * 3600 * 1000;
  const late: Order[] = [];
  const none: Order[] = [];
  const byDay = new Map<number, Order[]>();

  for (const o of orders) {
    if (!o.delivery_date) {
      none.push(o);
      continue;
    }
    const start = dayStart(new Date(o.delivery_date));
    if (start < today) {
      late.push(o);
    } else {
      byDay.set(start, [...(byDay.get(start) || []), o]);
    }
  }

  const groups: Group[] = [];
  if (late.length) groups.push({ key: 'late', title: '⚠️ Просрочено', tone: 'late', orders: late });
  Array.from(byDay.keys())
    .sort((a, b) => a - b)
    .forEach((start) => {
      let title: string;
      const label = new Date(start).toLocaleDateString('ru-RU', {
        weekday: 'long',
        day: 'numeric',
        month: 'long',
      });
      if (start === today) title = `🔥 Сегодня — ${label}`;
      else if (start === today + day) title = `Завтра — ${label}`;
      else title = label.charAt(0).toUpperCase() + label.slice(1);
      groups.push({
        key: String(start),
        title,
        tone: start === today ? 'today' : 'normal',
        orders: byDay.get(start) || [],
      });
    });
  if (none.length) groups.push({ key: 'none', title: '🕓 Без даты', tone: 'none', orders: none });
  return groups;
}

export const CalendarView: React.FC<CalendarViewProps> = ({ orders, loading, onOpenClient }) => {
  const groups = buildGroups(orders);

  if (loading && orders.length === 0) {
    return <div className="page-empty">Загрузка...</div>;
  }
  if (orders.length === 0) {
    return (
      <div className="page-empty">
        <div className="empty-order-icon">📅</div>
        <h3>Активных заказов нет</h3>
        <p>Создайте заказ в карточке клиента — он появится здесь по дате готовности.</p>
      </div>
    );
  }

  return (
    <div className="page-body">
      <div className="calendar-summary">Активных заказов: {orders.length}</div>
      {groups.map((g) => (
        <div key={g.key} className={`cal-group tone-${g.tone}`}>
          <h3 className="cal-group-title">
            {g.title} <span>({g.orders.length})</span>
          </h3>
          {g.orders.map((o) => (
            <div key={o.id} className="cal-order" onClick={() => onOpenClient(o.client_id)}>
              <div className="cal-time">
                {o.delivery_date
                  ? new Date(o.delivery_date).toLocaleTimeString('ru-RU', {
                      hour: '2-digit',
                      minute: '2-digit',
                    })
                  : '—'}
              </div>
              <div className="cal-main">
                <div className="cal-title">
                  {o.client_name}
                  <span className={`status-pill status-${o.status}`}>{ORDER_STATUS_LABELS[o.status]}</span>
                </div>
                <div className="cal-desc">
                  {o.cake_description || 'Без описания'}
                  {o.weight_kg > 0 && ` • ${o.weight_kg} кг`}
                  {o.filling && ` • ${o.filling}`}
                </div>
                <div className="cal-meta">
                  {o.delivery_type === 'delivery'
                    ? `🚗 Доставка${o.delivery_address ? `: ${o.delivery_address}` : ''}`
                    : '🏪 Самовывоз'}
                  {o.price > 0 && ` • ${formatMoney(o.price)}`}
                  {o.payment_status !== 'paid' && o.price > 0 && (
                    <strong> • к доплате {formatMoney(Math.max(o.price - o.prepayment, 0))}</strong>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
};

