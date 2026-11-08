import React, { useEffect, useState } from 'react';
import { Client, clientDisplayName } from '../types';

interface ClientCardProps {
  client: Client;
  onSaveProfile: (data: {
    custom_name: string;
    phone: string;
    address: string;
    description: string;
  }) => Promise<void>;
}

export const ClientCard: React.FC<ClientCardProps> = ({ client, onSaveProfile }) => {
  const [customName, setCustomName] = useState(client.custom_name || clientDisplayName(client));
  const [phone, setPhone] = useState(client.phone || '');
  const [address, setAddress] = useState(client.address || '');
  const [description, setDescription] = useState(client.description || '');
  const [saving, setSaving] = useState(false);
  const [savedToast, setSavedToast] = useState(false);

  useEffect(() => {
    setCustomName(client.custom_name || clientDisplayName(client));
    setPhone(client.phone || '');
    setAddress(client.address || '');
    setDescription(client.description || '');
  }, [client.id, client.custom_name, client.phone, client.address, client.description]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      await onSaveProfile({
        custom_name: customName,
        phone,
        address,
        description,
      });
      setSavedToast(true);
      setTimeout(() => setSavedToast(false), 2500);
    } finally {
      setSaving(false);
    }
  };

  return (
    <form className="crm-section-card" onSubmit={handleSubmit}>
      <div className="section-title-row">
        <h3>👤 Атрибуты клиента</h3>
        <span className="max-id-badge">MAX ID: {client.max_user_id}</span>
      </div>

      <div className="form-grid">
        <label>
          Имя клиента
          <input
            type="text"
            value={customName}
            onChange={(e) => setCustomName(e.target.value)}
            placeholder="Имя Фамилия"
          />
        </label>

        <label>
          Телефон
          <input
            type="text"
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
          Описание клиента (предпочтения, аллергии, заметки)
          <textarea
            rows={3}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Например: любит ягодные начинки, аллергия на орехи и мёд, постоянная скидка 5%..."
          />
        </label>
      </div>

      <div className="form-footer-row">
        {savedToast && <span className="save-toast">✅ Сохранено!</span>}
        <button type="submit" className="btn-primary" disabled={saving}>
          {saving ? 'Сохранение...' : '💾 Сохранить профиль клиента'}
        </button>
      </div>
    </form>
  );
};

