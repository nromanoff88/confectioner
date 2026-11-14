import React, { useEffect, useState } from 'react';
import { Client, ClientInput, TAG_PRESETS } from '../types';

interface ClientCardProps {
  client: Client;
  onSave: (data: ClientInput) => Promise<void>;
  onDelete: () => Promise<void>;
  onRequestMaxLink: () => Promise<string>;
  onUnlinkMax: () => Promise<void>;
}

function openMaxUrl(url: string) {
  if (window.WebApp?.openMaxLink) {
    window.WebApp.openMaxLink(url);
  } else {
    window.open(url, '_blank', 'noopener');
  }
}

function openClientInMax(client: Client) {
  if (client.max_username) {
    openMaxUrl(`https://max.ru/${client.max_username}`);
  } else if (client.max_user_id > 0) {
    openMaxUrl(`https://max.ru/u/${client.max_user_id}`);
  }
}

export const ClientCard: React.FC<ClientCardProps> = ({
  client,
  onSave,
  onDelete,
  onRequestMaxLink,
  onUnlinkMax,
}) => {
  const [name, setName] = useState(client.name);
  const [phone, setPhone] = useState(client.phone);
  const [address, setAddress] = useState(client.address);
  const [maxUsername, setMaxUsername] = useState(client.max_username);
  const [description, setDescription] = useState(client.description);
  const [tags, setTags] = useState<string[]>(client.tags);
  const [customTag, setCustomTag] = useState('');
  const [saving, setSaving] = useState(false);
  const [linking, setLinking] = useState(false);
  const [botLinkUrl, setBotLinkUrl] = useState('');
  const [toast, setToast] = useState('');

  useEffect(() => {
    setName(client.name);
    setPhone(client.phone);
    setAddress(client.address);
    setMaxUsername(client.max_username);
    setDescription(client.description);
    setTags(client.tags);
    setBotLinkUrl('');
  }, [client]);

  const toggleTag = (t: string) =>
    setTags((prev) => (prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]));

  const addCustomTag = () => {
    const t = customTag.trim();
    if (t && !tags.includes(t)) setTags([...tags, t]);
    setCustomTag('');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      await onSave({
        name,
        phone,
        address,
        description,
        max_username: maxUsername,
        tags,
      });
      setToast('✅ Сохранено!');
    } catch (err) {
      setToast(err instanceof Error ? `⚠️ ${err.message}` : '⚠️ Ошибка сохранения');
    } finally {
      setSaving(false);
      setTimeout(() => setToast(''), 2500);
    }
  };

  const handleDelete = async () => {
    if (
      window.confirm(
        `Удалить клиента «${client.name}» вместе со всеми заказами, фото и заметками? Это нельзя отменить.`
      )
    ) {
      await onDelete();
    }
  };

  const handleRequestLink = async () => {
    setLinking(true);
    try {
      const url = await onRequestMaxLink();
      setBotLinkUrl(url);
      setToast('📩 Бот ждёт контакт в чате MAX');
    } catch (err) {
      setToast(err instanceof Error ? `⚠️ ${err.message}` : '⚠️ Ошибка');
    } finally {
      setLinking(false);
    }
  };

  const handleUnlink = async () => {
    if (!window.confirm(`Отвязать контакт MAX от клиента «${client.name}»?`)) return;
    setLinking(true);
    try {
      await onUnlinkMax();
      setToast('Контакт MAX отвязан');
    } catch (err) {
      setToast(err instanceof Error ? `⚠️ ${err.message}` : '⚠️ Ошибка');
    } finally {
      setLinking(false);
    }
  };

  const isMaxLinked = client.max_user_id > 0 || Boolean(client.max_username);
  const presetsAndCustom = Array.from(new Set([...TAG_PRESETS, ...tags]));

  return (
    <form className="crm-section-card" onSubmit={handleSubmit}>
      <div className="section-title-row">
        <h3>👤 Данные клиента</h3>
        <span className="max-id-badge">
          Заказов: {client.orders_count} • на сумму {Math.round(client.total_spent).toLocaleString('ru-RU')} ₽
        </span>
      </div>

      <div className="quick-actions">
        {client.phone && (
          <a className="btn-small-outline" href={`tel:${client.phone.replace(/[^+\d]/g, '')}`}>
            📞 Позвонить
          </a>
        )}
        {isMaxLinked && (
          <button type="button" className="btn-small-outline" onClick={() => openClientInMax(client)}>
            💬 Открыть в MAX
          </button>
        )}
        <button
          type="button"
          className="btn-small-outline"
          disabled={linking}
          onClick={handleRequestLink}
        >
          🔗 {isMaxLinked ? 'Перепривязать контакт через бота' : 'Привязать контакт MAX через бота'}
        </button>
        {isMaxLinked && (
          <button type="button" className="btn-small-outline" disabled={linking} onClick={handleUnlink}>
            ✕ Отвязать MAX
          </button>
        )}
      </div>

      {isMaxLinked && (
        <div className="max-link-banner linked">
          <span>
            ✅ <strong>Привязан к MAX:</strong>{' '}
            {client.max_username ? `@${client.max_username}` : ''}
            {client.max_username && client.max_user_id > 0 ? ' • ' : ''}
            {client.max_user_id > 0 ? `ID ${client.max_user_id}` : ''}
          </span>
        </div>
      )}

      {botLinkUrl && (
        <div className="max-link-banner pending">
          <div>
            <strong>📩 Бот ожидает контакт для «{client.name}»</strong>
            <p style={{ margin: '4px 0 8px', fontSize: 13 }}>
              Перейдите в чат с ботом и отправьте <strong>Контакт</strong> (скрепка → Контакт) или{' '}
              <strong>перешлите любое сообщение</strong> от этого клиента.
            </p>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              <button
                type="button"
                className="btn-primary"
                onClick={() => {
                  if (window.WebApp?.close) {
                    window.WebApp.close();
                  } else {
                    openMaxUrl(botLinkUrl);
                  }
                }}
              >
                Перейти в чат с ботом
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="form-grid">
        <label>
          Имя клиента *
          <input type="text" required value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          Телефон
          <input
            type="tel"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            placeholder="+7 (999) 000-00-00"
          />
        </label>
        <label className="full-width">
          Адрес доставки по умолчанию
          <input
            type="text"
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            placeholder="Город, улица, дом, подъезд, квартира"
          />
        </label>
        <label className="full-width">
          Ник в MAX (необязательно, для кнопки «Открыть в MAX»)
          <input
            type="text"
            value={maxUsername}
            onChange={(e) => setMaxUsername(e.target.value)}
            placeholder="username без @"
          />
        </label>
        <label className="full-width">
          Описание клиента (предпочтения, аллергии, скидки)
          <textarea
            rows={3}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Например: любит ягодные начинки, аллергия на орехи, скидка 5%..."
          />
        </label>
      </div>

      <div>
        <div className="field-label">Теги</div>
        <div className="tag-row">
          {presetsAndCustom.map((t) => (
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
        <div className="tag-add-row">
          <input
            type="text"
            placeholder="Свой тег..."
            value={customTag}
            onChange={(e) => setCustomTag(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                addCustomTag();
              }
            }}
          />
          <button type="button" className="btn-small-outline" onClick={addCustomTag}>
            + Добавить
          </button>
        </div>
      </div>

      <div className="form-footer-row">
        {toast && <span className="save-toast">{toast}</span>}
        <button type="button" className="btn-danger-outline" onClick={handleDelete}>
          🗑 Удалить клиента
        </button>
        <button type="submit" className="btn-primary" disabled={saving}>
          {saving ? 'Сохранение...' : '💾 Сохранить'}
        </button>
      </div>
    </form>
  );
};
