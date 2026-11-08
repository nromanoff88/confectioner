import React, { useState } from 'react';
import { Client, clientDisplayName, ORDER_STATUS_LABELS } from '../types';

interface ChatListProps {
  clients: Client[];
  selectedClientId: number | null;
  search: string;
  filter: 'all' | 'unread' | 'active_order';
  onSearchChange: (val: string) => void;
  onFilterChange: (val: 'all' | 'unread' | 'active_order') => void;
  onSelectClient: (client: Client) => void;
  onCreateClient: (data: {
    custom_name: string;
    phone: string;
    address: string;
    description: string;
  }) => Promise<void>;
}

export const ChatList: React.FC<ChatListProps> = ({
  clients,
  selectedClientId,
  search,
  filter,
  onSearchChange,
  onFilterChange,
  onSelectClient,
  onCreateClient,
}) => {
  const [showNewModal, setShowNewModal] = useState(false);
  const [newName, setNewName] = useState('');
  const [newPhone, setNewPhone] = useState('');
  const [newAddress, setNewAddress] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [saving, setSaving] = useState(false);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newName.trim()) return;
    setSaving(true);
    try {
      await onCreateClient({
        custom_name: newName.trim(),
        phone: newPhone.trim(),
        address: newAddress.trim(),
        description: newDesc.trim(),
      });
      setNewName('');
      setNewPhone('');
      setNewAddress('');
      setNewDesc('');
      setShowNewModal(false);
    } finally {
      setSaving(false);
    }
  };

  const formatTime = (iso: string) => {
    if (!iso) return '';
    const d = new Date(iso);
    const now = new Date();
    if (d.toDateString() === now.toDateString()) {
      return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });
    }
    return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' });
  };

  return (
    <div className="chat-list-panel">
      <div className="chat-list-header">
        <div className="brand-row">
          <div className="brand-title">
            <span className="brand-icon">🎂</span>
            <div>
              <h1>Панель Кондитера</h1>
              <span className="brand-sub">MAX CRM • Диалоги и заказы</span>
            </div>
          </div>
          <button
            className="btn-icon-primary"
            onClick={() => setShowNewModal(true)}
            title="Добавить клиента вручную"
          >
            + Клиент
          </button>
        </div>

        <div className="search-box">
          <input
            type="text"
            placeholder="🔍 Поиск по имени, телефону, заметкам..."
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
          />
          {search && (
            <button className="clear-search" onClick={() => onSearchChange('')}>
              ✕
            </button>
          )}
        </div>

        <div className="filter-tabs">
          <button
            className={filter === 'all' ? 'active' : ''}
            onClick={() => onFilterChange('all')}
          >
            Все ({clients.length})
          </button>
          <button
            className={filter === 'unread' ? 'active' : ''}
            onClick={() => onFilterChange('unread')}
          >
            Новые
          </button>
          <button
            className={filter === 'active_order' ? 'active' : ''}
            onClick={() => onFilterChange('active_order')}
          >
            🎂 С заказом
          </button>
        </div>
      </div>

      <div className="chat-list-items">
        {clients.length === 0 ? (
          <div className="empty-list">
            <p>Клиентов не найдено</p>
            <small>Когда клиент напишет боту в MAX, его чат появится здесь автоматически.</small>
          </div>
        ) : (
          clients.map((c) => {
            const name = clientDisplayName(c);
            const initials = name.slice(0, 2).toUpperCase();
            const isSelected = c.id === selectedClientId;

            return (
              <div
                key={c.id}
                className={`chat-item ${isSelected ? 'selected' : ''} ${
                  c.unread_count > 0 ? 'unread' : ''
                }`}
                onClick={() => onSelectClient(c)}
              >
                <div className="chat-avatar">
                  {c.avatar_url ? <img src={c.avatar_url} alt={name} /> : <span>{initials}</span>}
                  {c.unread_count > 0 && <span className="unread-dot">{c.unread_count}</span>}
                </div>

                <div className="chat-item-body">
                  <div className="chat-item-top">
                    <span className="chat-name">{name}</span>
                    <span className="chat-time">{formatTime(c.last_message_at)}</span>
                  </div>

                  {c.phone && <div className="chat-phone-line">📞 {c.phone}</div>}

                  <div className="chat-preview">
                    {c.last_message_text || 'Нет сообщений'}
                  </div>

                  {c.active_order && (
                    <div className={`chat-order-badge status-${c.active_order.status}`}>
                      <span>{ORDER_STATUS_LABELS[c.active_order.status] || c.active_order.status}</span>
                      {c.active_order.weight_kg > 0 && (
                        <span> • {c.active_order.weight_kg} кг</span>
                      )}
                      {c.active_order.price > 0 && <span> • {c.active_order.price} ₽</span>}
                    </div>
                  )}
                </div>
              </div>
            );
          })
        )}
      </div>

      {showNewModal && (
        <div className="modal-backdrop" onClick={() => setShowNewModal(false)}>
          <form
            className="modal-card"
            onClick={(e) => e.stopPropagation()}
            onSubmit={handleCreate}
          >
            <h3>Новый клиент</h3>
            <label>
              Имя клиента *
              <input
                type="text"
                required
                placeholder="Например: Мария Иванова"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
              />
            </label>
            <label>
              Телефон
              <input
                type="text"
                placeholder="+7 (999) 000-00-00"
                value={newPhone}
                onChange={(e) => setNewPhone(e.target.value)}
              />
            </label>
            <label>
              Адрес доставки
              <input
                type="text"
                placeholder="Город, улица, дом, квартира"
                value={newAddress}
                onChange={(e) => setNewAddress(e.target.value)}
              />
            </label>
            <label>
              Описание / заметки о клиенте
              <textarea
                rows={3}
                placeholder="Вкусовые предпочтения, аллергии..."
                value={newDesc}
                onChange={(e) => setNewDesc(e.target.value)}
              />
            </label>
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={() => setShowNewModal(false)}>
                Отмена
              </button>
              <button type="submit" className="btn-primary" disabled={saving}>
                {saving ? 'Сохранение...' : 'Создать'}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};

