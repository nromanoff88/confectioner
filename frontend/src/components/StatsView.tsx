import React, { useEffect, useState } from 'react';
import { api } from '../api/client';
import { formatMoney, Stats } from '../types';

function currentMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

export const StatsView: React.FC<{ onOpenClient: (clientId: number) => void }> = ({ onOpenClient }) => {
  const [month, setMonth] = useState(currentMonth());
  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    setError('');
    api
      .getStats(month)
      .then((s) => {
        if (!cancelled) setStats(s);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Ошибка загрузки');
      });
    return () => {
      cancelled = true;
    };
  }, [month]);

  return (
    <div className="page-body">
      <div className="stats-header">
        <h2>📊 Статистика</h2>
        <input type="month" value={month} onChange={(e) => e.target.value && setMonth(e.target.value)} />
      </div>
      {error && <div className="form-error">{error}</div>}
      {stats && (
        <>
          <div className="stats-grid">
            <div className="stat-card accent">
              <span>Выручка за месяц</span>
              <strong>{formatMoney(stats.revenue)}</strong>
            </div>
            <div className="stat-card">
              <span>Выполнено заказов</span>
              <strong>{stats.orders_completed}</strong>
            </div>
            <div className="stat-card">
              <span>Продано тортов (кг)</span>
              <strong>{stats.weight_kg.toFixed(1)}</strong>
            </div>
            <div className="stat-card">
              <span>Средний чек</span>
              <strong>{formatMoney(stats.avg_check)}</strong>
            </div>
            <div className="stat-card">
              <span>Новых клиентов</span>
              <strong>{stats.new_clients}</strong>
            </div>
            <div className="stat-card">
              <span>Заказов в работе сейчас</span>
              <strong>{stats.active_orders}</strong>
            </div>
            <div className="stat-card warn">
              <span>К доплате по активным</span>
              <strong>{formatMoney(stats.outstanding)}</strong>
            </div>
          </div>

          <div className="crm-section-card">
            <h3>🏆 Топ клиентов месяца</h3>
            {stats.top_clients.length === 0 ? (
              <div className="photo-empty-hint">
                Пока нет выполненных заказов за этот месяц. Заказ попадает в статистику, когда получает статус
                «Выполнен».
              </div>
            ) : (
              <div className="top-list">
                {stats.top_clients.map((t, i) => (
                  <div key={t.client_id} className="top-row" onClick={() => onOpenClient(t.client_id)}>
                    <span className="top-rank">{i + 1}</span>
                    <span className="top-name">{t.name}</span>
                    <span className="top-orders">{t.orders} зак.</span>
                    <strong>{formatMoney(t.revenue)}</strong>
                  </div>
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
};

