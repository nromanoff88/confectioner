import React, { useState } from 'react';
import { Client, ClientInput, clientDisplayName, ORDER_STATUS_LABELS, TAG_PRESETS } from '../types';

interface ClientListProps {
  clients: Client[];
  selectedClientId: number | null;
  search: string;
  tag: string;
  allTags: string[];
  autoOpenNew: boolean;
  onAutoOpenHandled: () => void;
  onSearchChange: (val: string) => void;
  onTagChange: (val: string) => void;
  onSelectClient: (client: Client) => void;
  onCreateClient: (data: ClientInput) => Promise<void>;
}

export const ClientList: React.FC<ClientListProps> = ({
  clients,
  selectedClientId,
  search,
  tag,
  allTags,
  autoOpenNew,
  onAutoOpenHandled,
  onSearchChange,
  onTagChange,
  onSelectClient,
  onCreateClient,
}) => {
  const [showNew, setShowNew] = useState(false);
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [address, setAddress] = useState('');
  const [description, setDescription] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  React.useEffect(() => {
    if (autoOpenNew) {
      setShowNew(true);
      onAutoOpenHandled();
    }
  }, [autoOpenNew, onAutoOpenHandled]);

  const toggleTag = (t: string) =>
    setTags((prev) => (prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]));

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    setError('');
    try {
      await onCreateClient({
        name: name.trim(),
        phone: phone.trim(),
        address: address.trim(),
        description: description.trim(),
        max_username: '',
        tags,
      });
      setName('');
      setPhone('');
      setAddress('');
      setDescription('');
      setTags([]);
      setShowNew(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Не удалось сохранить');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="chat-list-panel">
      <div className="chat-list-header">
        <div className="brand-row">
          <div className="brand-title">
            <div>
              <h1>Клиенты ({clients.length})</h1>
              <span className="brand-sub">Ваша база клиентов и заказов</span>
            </div>
          </div>
          <button className="btn-icon-primary" onClick={() => setShowNew(true)}>
            + Записать клиента
          </button>
        </div>

        <div className="search-box">
          <input
            type="text"
            placeholder="🔍 Имя, телефон, адрес, заметки, тег..."
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
          />
          {search && (
            <button className="clear-search" onClick={() => onSearchChange('')}>
              ✕
            </button>
          )}
        </div>

        {allTags.length > 0 && (
          <div className="tag-row">
            <button className={`tag-chip ${tag === '' ? 'on' : ''}`} onClick={() => onTagChange('')}>
              Все
            </button>
            {allTags.map((t) => (
              <button
                key={t}
                className={`tag-chip ${tag === t ? 'on' : ''}`}
                onClick={() => onTagChange(tag === t ? '' : t)}
              >
                {t}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="chat-list-items">
        {clients.length === 0 ? (
          <div className="empty-list">
            <p>Клиентов не найдено</p>
            <small>Нажмите «Записать клиента», чтобы добавить первого.</small>
          </div>
        ) : (
          clients.map((c) => {
            const display = clientDisplayName(c);
            return (
              <div
                key={c.id}
                className={`chat-item ${c.id === selectedClientId ? 'selected' : ''}`}
                onClick={() => onSelectClient(c)}
              >
                <div className="chat-avatar">
                  <span>{display.slice(0, 2).toUpperCase()}</span>
                </div>
                <div className="chat-item-body">
                  <div className="chat-item-top">
                    <span className="chat-name">{display}</span>
                    {c.orders_count > 0 && <span className="chat-time">🎂 {c.orders_count}</span>}
                  </div>
                  {c.phone && <div className="chat-phone-line">📞 {c.phone}</div>}
                  {c.tags.length > 0 && (
                    <div className="tag-row small">
                      {c.tags.map((t) => (
                        <span key={t} className="tag-chip static">
                          {t}
                        </span>
                      ))}
                    </div>
                  )}
                  {c.active_order && (
                    <div className={`chat-order-badge status-${c.active_order.status}`}>
                      <span>{ORDER_STATUS_LABELS[c.active_order.status]}</span>
                      {c.active_order.delivery_date && (
                        <span>
                          {' '}
                          •{' '}
                          {new Date(c.active_order.delivery_date).toLocaleString('ru-RU', {
                            day: '2-digit',
                            month: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit',
                          })}
                        </span>
                      )}
                    </div>
                  )}
                </div>
              </div>
            );
          })
        )}
      </div>

      {showNew && (
        <div className="modal-backdrop" onClick={() => setShowNew(false)}>
          <form className="modal-card" onClick={(e) => e.stopPropagation()} onSubmit={handleCreate}>
            <h3>Новый клиент</h3>
            <label>
              Имя клиента *
              <input
                type="text"
                required
                autoFocus
                placeholder="Например: Мария Иванова"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label>
              Телефон
              <input
                type="tel"
                placeholder="+7 (999) 000-00-00"
                value={phone}
                onChange={(e) => setPhone(e.target.value)}
              />
            </label>
            <label>
              Адрес доставки
              <input
                type="text"
                placeholder="Город, улица, дом, квартира"
                value={address}
                onChange={(e) => setAddress(e.target.value)}
              />
            </label>
            <label>
              Заметки о клиенте
              <textarea
                rows={3}
                placeholder="Вкусовые предпочтения, аллергии..."
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </label>
            <div className="tag-row">
              {TAG_PRESETS.map((t) => (
                <button
                  type="button"
                  key={t}
                  className={`tag-chip ${tags.includes(t) ? 'on' : ''}`}
                  onClick={() => toggleTag(t)}
                >
                  {t}
                </button>
              ))}
            </div>
            {error && <div className="form-error">{error}</div>}
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={() => setShowNew(false)}>
                Отмена
              </button>
              <button type="submit" className="btn-primary" disabled={saving}>
                {saving ? 'Сохранение...' : 'Записать'}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};

