import React, { useEffect, useRef, useState } from 'react';
import {
  Client,
  DeliveryType,
  Order,
  ORDER_STATUS_LABELS,
  OrderInput,
  OrderStatus,
  PAYMENT_STATUS_LABELS,
  PaymentStatus,
  REMIND_OPTIONS,
} from '../types';

interface ActiveOrderPanelProps {
  client: Client;
  order: Order | null;
  startCreating: boolean;
  onCreatingHandled: () => void;
  onSaveOrder: (payload: OrderInput) => Promise<void>;
  onChangeStatus: (orderId: number, status: OrderStatus) => Promise<void>;
  onDeleteOrder: (orderId: number) => Promise<void>;
  onUploadPhoto: (orderId: number, kind: 'reference' | 'result', file: File) => Promise<void>;
  onDeletePhoto: (orderId: number, photoId: number) => Promise<void>;
  onPreviewImage: (url: string, caption?: string) => void;
}

function toLocalDatetimeInput(iso?: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(
    d.getMinutes()
  )}`;
}

export const ActiveOrderPanel: React.FC<ActiveOrderPanelProps> = ({
  client,
  order,
  startCreating,
  onCreatingHandled,
  onSaveOrder,
  onChangeStatus,
  onDeleteOrder,
  onUploadPhoto,
  onDeletePhoto,
  onPreviewImage,
}) => {
  const [isCreating, setIsCreating] = useState(false);
  const [status, setStatus] = useState<OrderStatus>('new');
  const [cakeDescription, setCakeDescription] = useState('');
  const [filling, setFilling] = useState('');
  const [decorInscription, setDecorInscription] = useState('');
  const [weightKg, setWeightKg] = useState('2.0');
  const [price, setPrice] = useState('');
  const [prepayment, setPrepayment] = useState('');
  const [paymentStatus, setPaymentStatus] = useState<PaymentStatus>('unpaid');
  const [deliveryType, setDeliveryType] = useState<DeliveryType>('delivery');
  const [deliveryAddress, setDeliveryAddress] = useState('');
  const [deliveryDate, setDeliveryDate] = useState('');
  const [remindBefore, setRemindBefore] = useState(120);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState('');
  const [uploadingKind, setUploadingKind] = useState<'reference' | 'result' | null>(null);

  const refFileInput = useRef<HTMLInputElement | null>(null);
  const resFileInput = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (startCreating && !order) {
      setIsCreating(true);
      onCreatingHandled();
    }
  }, [startCreating, order, onCreatingHandled]);

  useEffect(() => {
    if (order) {
      setIsCreating(false);
      setStatus(order.status);
      setCakeDescription(order.cake_description || '');
      setFilling(order.filling || '');
      setDecorInscription(order.decor_inscription || '');
      setWeightKg(order.weight_kg ? String(order.weight_kg) : '');
      setPrice(order.price ? String(order.price) : '');
      setPrepayment(order.prepayment ? String(order.prepayment) : '');
      setPaymentStatus(order.payment_status || 'unpaid');
      setDeliveryType(order.delivery_type || 'pickup');
      setDeliveryAddress(order.delivery_address || client.address || '');
      setDeliveryDate(toLocalDatetimeInput(order.delivery_date));
      setRemindBefore(order.remind_before_min ?? 120);
    } else {
      setStatus('new');
      setCakeDescription('');
      setFilling('');
      setDecorInscription('');
      setWeightKg('2.0');
      setPrice('');
      setPrepayment('');
      setPaymentStatus('unpaid');
      setDeliveryType(client.address ? 'delivery' : 'pickup');
      setDeliveryAddress(client.address || '');
      setDeliveryDate('');
      setRemindBefore(120);
    }
  }, [order, client.id, client.address]);

  if (!order && !isCreating) {
    return (
      <div className="crm-section-card empty-order-box">
        <div className="empty-order-icon">🎂</div>
        <h3>Активного заказа нет</h3>
        <p>
          Запишите заказ клиента: что хочет, вес, начинка, дата, предоплата и фото-референсы. Бот напомнит вам о
          доставке заранее.
        </p>
        <button className="btn-primary" onClick={() => setIsCreating(true)}>
          + Новый заказ
        </button>
      </div>
    );
  }

  const flash = (msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(''), 2500);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      await onSaveOrder({
        id: order?.id,
        status,
        cake_description: cakeDescription,
        filling,
        decor_inscription: decorInscription,
        weight_kg: parseFloat(weightKg) || 0,
        price: parseFloat(price) || 0,
        prepayment: parseFloat(prepayment) || 0,
        payment_status: paymentStatus,
        delivery_type: deliveryType,
        delivery_address: deliveryAddress,
        delivery_date: deliveryDate ? new Date(deliveryDate).toISOString() : '',
        remind_before_min: remindBefore,
      });
      flash('✅ Заказ сохранён!');
    } catch (err) {
      flash(err instanceof Error ? `⚠️ ${err.message}` : '⚠️ Ошибка сохранения');
    } finally {
      setSaving(false);
    }
  };

  const handleQuickStatus = async (next: OrderStatus) => {
    if (!order) return;
    setSaving(true);
    try {
      await onChangeStatus(order.id, next);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!order) return;
    if (window.confirm(`Удалить заказ #${order.id} вместе с фотографиями? Это нельзя отменить.`)) {
      await onDeleteOrder(order.id);
    }
  };

  const handleFileChange = async (kind: 'reference' | 'result', file?: File | null) => {
    if (!file || !order) return;
    setUploadingKind(kind);
    try {
      await onUploadPhoto(order.id, kind, file);
    } catch (err) {
      flash(err instanceof Error ? `⚠️ ${err.message}` : '⚠️ Не удалось загрузить фото');
    } finally {
      setUploadingKind(null);
    }
  };

  const numPrice = parseFloat(price) || 0;
  const numPrepay = parseFloat(prepayment) || 0;
  const remaining = paymentStatus === 'paid' ? 0 : Math.max(0, numPrice - numPrepay);

  const refPhotos = (order?.photos || []).filter((p) => p.kind === 'reference');
  const resPhotos = (order?.photos || []).filter((p) => p.kind === 'result');

  const statusSteps: OrderStatus[] = [
    'new',
    'awaiting_prepayment',
    'in_progress',
    'ready',
    'completed',
    'cancelled',
  ];

  const renderPhotos = (photos: typeof refPhotos, alt: string) =>
    photos.length === 0 ? null : (
      <div className="photo-grid">
        {photos.map((p) => (
          <div key={p.id} className="photo-thumb-card">
            <img src={p.url} alt={alt} onClick={() => onPreviewImage(p.url, p.caption || alt)} />
            {order && (
              <button
                type="button"
                className="photo-delete-btn"
                onClick={() => onDeletePhoto(order.id, p.id)}
                title="Удалить фото"
              >
                ✕
              </button>
            )}
          </div>
        ))}
      </div>
    );

  return (
    <div className="crm-section-card active-order-card">
      <div className="section-title-row">
        <h3>🎂 {order ? `Активный заказ #${order.id}` : 'Новый заказ'}</h3>
        {order && (
          <span className={`status-pill status-${order.status}`}>{ORDER_STATUS_LABELS[order.status]}</span>
        )}
      </div>

      {order && (
        <div className="status-stepper">
          <div className="stepper-hint">
            Статус заказа. «Выполнен» и «Отменён» переносят заказ в историю клиента и в статистику.
          </div>
          <div className="status-buttons-grid">
            {statusSteps.map((st) => (
              <button
                key={st}
                type="button"
                disabled={saving}
                className={`status-step-btn status-${st} ${order.status === st ? 'current' : ''}`}
                onClick={() => handleQuickStatus(st)}
              >
                {ORDER_STATUS_LABELS[st]}
              </button>
            ))}
          </div>
        </div>
      )}

      <form onSubmit={handleSubmit}>
        <div className="form-grid">
          <label className="full-width">
            Что хочет клиент (описание торта / повод) *
            <textarea
              rows={2}
              required
              placeholder="Например: двухъярусный торт в розовом велюре со свежими ягодами..."
              value={cakeDescription}
              onChange={(e) => setCakeDescription(e.target.value)}
            />
          </label>

          <label>
            Начинка / вкус
            <input
              type="text"
              placeholder="Манго-маракуйя / фисташка-малина"
              value={filling}
              onChange={(e) => setFilling(e.target.value)}
            />
          </label>

          <label>
            Декор и надпись
            <input
              type="text"
              placeholder="С Днём Рождения, мама!"
              value={decorInscription}
              onChange={(e) => setDecorInscription(e.target.value)}
            />
          </label>

          <label>
            Вес (кг)
            <input
              type="number"
              step="0.1"
              min="0"
              value={weightKg}
              onChange={(e) => setWeightKg(e.target.value)}
            />
          </label>

          <label>
            Дата и время готовности / доставки
            <input type="datetime-local" value={deliveryDate} onChange={(e) => setDeliveryDate(e.target.value)} />
          </label>

          <label>
            Напомнить мне в боте
            <select value={remindBefore} onChange={(e) => setRemindBefore(parseInt(e.target.value, 10))}>
              {REMIND_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>

          <label>
            Способ получения
            <select value={deliveryType} onChange={(e) => setDeliveryType(e.target.value as DeliveryType)}>
              <option value="delivery">🚗 Доставка по адресу</option>
              <option value="pickup">🏪 Самовывоз</option>
            </select>
          </label>

          {deliveryType === 'delivery' && (
            <label className="full-width">
              Адрес доставки заказа
              <input
                type="text"
                value={deliveryAddress}
                onChange={(e) => setDeliveryAddress(e.target.value)}
              />
            </label>
          )}

          <label>
            Стоимость (₽)
            <input
              type="number"
              step="50"
              min="0"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
            />
          </label>

          <label>
            Предоплата (₽)
            <input
              type="number"
              step="50"
              min="0"
              value={prepayment}
              onChange={(e) => setPrepayment(e.target.value)}
            />
          </label>

          <label className="full-width">
            Статус оплаты (к доплате: {remaining} ₽)
            <select value={paymentStatus} onChange={(e) => setPaymentStatus(e.target.value as PaymentStatus)}>
              {(Object.keys(PAYMENT_STATUS_LABELS) as PaymentStatus[]).map((k) => (
                <option key={k} value={k}>
                  {PAYMENT_STATUS_LABELS[k]}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="form-footer-row">
          {!order && (
            <button type="button" className="btn-secondary" onClick={() => setIsCreating(false)}>
              Отмена
            </button>
          )}
          {order && (
            <button type="button" className="btn-danger-outline" onClick={handleDelete}>
              🗑 Удалить
            </button>
          )}
          {toast && <span className="save-toast">{toast}</span>}
          <button type="submit" className="btn-primary" disabled={saving}>
            {saving ? 'Сохранение...' : order ? '💾 Сохранить заказ' : '✨ Создать заказ'}
          </button>
        </div>
      </form>

      {order ? (
        <div className="order-photos-section">
          <div className="photo-block">
            <div className="photo-block-header">
              <h4>📌 Референсы от клиента ({refPhotos.length})</h4>
              <input
                ref={refFileInput}
                type="file"
                accept="image/*"
                style={{ display: 'none' }}
                onChange={(e) => {
                  void handleFileChange('reference', e.target.files?.[0]);
                  if (refFileInput.current) refFileInput.current.value = '';
                }}
              />
              <button
                type="button"
                className="btn-small-outline"
                disabled={uploadingKind === 'reference'}
                onClick={() => refFileInput.current?.click()}
              >
                {uploadingKind === 'reference' ? 'Загрузка...' : '+ Загрузить'}
              </button>
            </div>
            {refPhotos.length === 0 ? (
              <div className="photo-empty-hint">
                Сохраните сюда фото-примеры, которые прислал клиент (скриншот из переписки в MAX).
              </div>
            ) : (
              renderPhotos(refPhotos, 'Референс')
            )}
          </div>

          <div className="photo-block">
            <div className="photo-block-header">
              <h4>🎂 Фото готового торта ({resPhotos.length})</h4>
              <input
                ref={resFileInput}
                type="file"
                accept="image/*"
                style={{ display: 'none' }}
                onChange={(e) => {
                  void handleFileChange('result', e.target.files?.[0]);
                  if (resFileInput.current) resFileInput.current.value = '';
                }}
              />
              <button
                type="button"
                className="btn-small-outline"
                disabled={uploadingKind === 'result'}
                onClick={() => resFileInput.current?.click()}
              >
                {uploadingKind === 'result' ? 'Загрузка...' : '+ Загрузить'}
              </button>
            </div>
            {resPhotos.length === 0 ? (
              <div className="photo-empty-hint">
                Сфотографируйте готовый торт — фото сохранится в истории заказов клиента.
              </div>
            ) : (
              renderPhotos(resPhotos, 'Готовый торт')
            )}
          </div>
        </div>
      ) : (
        <div className="photo-empty-hint">Фотографии можно добавить после создания заказа.</div>
      )}
    </div>
  );
};
