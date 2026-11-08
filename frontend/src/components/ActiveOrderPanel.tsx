import React, { useEffect, useRef, useState } from 'react';
import {
  Client,
  DeliveryType,
  Order,
  ORDER_STATUS_LABELS,
  OrderStatus,
  PAYMENT_STATUS_LABELS,
  PaymentStatus,
} from '../types';

interface ActiveOrderPanelProps {
  client: Client;
  order: Order | null;
  onSaveOrder: (payload: {
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
  }) => Promise<void>;
  onChangeStatus: (orderId: number, status: OrderStatus) => Promise<void>;
  onUploadPhoto: (orderId: number, kind: 'reference' | 'result', file: File) => Promise<void>;
  onDeletePhoto: (orderId: number, photoId: number) => Promise<void>;
  onPreviewImage: (url: string, caption?: string) => void;
}

function toLocalDatetimeInput(iso?: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours()
  )}:${pad(d.getMinutes())}`;
}

export const ActiveOrderPanel: React.FC<ActiveOrderPanelProps> = ({
  client,
  order,
  onSaveOrder,
  onChangeStatus,
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
  const [saving, setSaving] = useState(false);
  const [savedToast, setSavedToast] = useState(false);
  const [uploadingKind, setUploadingKind] = useState<'reference' | 'result' | null>(null);

  const refFileInput = useRef<HTMLInputElement | null>(null);
  const resFileInput = useRef<HTMLInputElement | null>(null);

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
    }
  }, [order, client.id, client.address]);

  if (!order && !isCreating) {
    return (
      <div className="crm-section-card empty-order-box">
        <div className="empty-order-icon">🎂</div>
        <h3>Активного заказа пока нет</h3>
        <p>
          Создайте новый заказ для клиента, чтобы фиксировать вес, начинку, декор, предоплату и
          прикреплять фото-референсы и фото готового торта.
        </p>
        <button className="btn-primary" onClick={() => setIsCreating(true)}>
          + Создать активный заказ
        </button>
      </div>
    );
  }

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
      });
      setSavedToast(true);
      setTimeout(() => setSavedToast(false), 2500);
    } finally {
      setSaving(false);
    }
  };

  const handleQuickStatus = async (nextStatus: OrderStatus) => {
    if (!order) {
      setStatus(nextStatus);
      return;
    }
    setSaving(true);
    try {
      await onChangeStatus(order.id, nextStatus);
    } finally {
      setSaving(false);
    }
  };

  const handleFileChange = async (kind: 'reference' | 'result', file?: File | null) => {
    if (!file || !order) return;
    setUploadingKind(kind);
    try {
      await onUploadPhoto(order.id, kind, file);
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

  return (
    <div className="crm-section-card active-order-card">
      <div className="section-title-row">
        <h3>
          🎂 {order ? `Активный заказ #${order.id}` : 'Новый заказ клиента'}
        </h3>
        {order && (
          <span className={`status-pill status-${order.status}`}>
            {ORDER_STATUS_LABELS[order.status]}
          </span>
        )}
      </div>

      {order && (
        <div className="status-stepper">
          <div className="stepper-hint">
            🔔 При переключении статуса бот автоматически отправит клиенту уведомление в MAX:
          </div>
          <div className="status-buttons-grid">
            {statusSteps.map((st) => (
              <button
                key={st}
                type="button"
                disabled={saving}
                className={`status-step-btn status-${st} ${
                  order.status === st ? 'current' : ''
                }`}
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
              placeholder="Например: Двухъярусный торт в розовом велюре со свежими ягодами..."
              value={cakeDescription}
              onChange={(e) => setCakeDescription(e.target.value)}
            />
          </label>

          <label>
            Начинка / вкус
            <input
              type="text"
              placeholder="Например: Манго-маракуйя / Фисташка-малина"
              value={filling}
              onChange={(e) => setFilling(e.target.value)}
            />
          </label>

          <label>
            Декор и надпись на торте
            <input
              type="text"
              placeholder="Например: С Днём Рождения, любимая мама!"
              value={decorInscription}
              onChange={(e) => setDecorInscription(e.target.value)}
            />
          </label>

          <label>
            Вес продукта (кг)
            <input
              type="number"
              step="0.1"
              min="0"
              placeholder="2.0"
              value={weightKg}
              onChange={(e) => setWeightKg(e.target.value)}
            />
          </label>

          <label>
            Дата и время готовности / доставки
            <input
              type="datetime-local"
              value={deliveryDate}
              onChange={(e) => setDeliveryDate(e.target.value)}
            />
          </label>

          <label>
            Стоимость заказа (₽)
            <input
              type="number"
              step="50"
              min="0"
              placeholder="6500"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
            />
          </label>

          <label>
            Сумма предоплаты (₽)
            <input
              type="number"
              step="50"
              min="0"
              placeholder="3000"
              value={prepayment}
              onChange={(e) => setPrepayment(e.target.value)}
            />
          </label>

          <label>
            Статус оплаты (К доплате: {remaining} ₽)
            <select
              value={paymentStatus}
              onChange={(e) => setPaymentStatus(e.target.value as PaymentStatus)}
            >
              {(Object.keys(PAYMENT_STATUS_LABELS) as PaymentStatus[]).map((k) => (
                <option key={k} value={k}>
                  {PAYMENT_STATUS_LABELS[k]}
                </option>
              ))}
            </select>
          </label>

          <label>
            Способ получения
            <select
              value={deliveryType}
              onChange={(e) => setDeliveryType(e.target.value as DeliveryType)}
            >
              <option value="delivery">🚗 Доставка по адресу</option>
              <option value="pickup">🏪 Самовывоз</option>
            </select>
          </label>

          {deliveryType === 'delivery' && (
            <label className="full-width">
              Адрес доставки заказа
              <input
                type="text"
                placeholder="Адрес доставки торта"
                value={deliveryAddress}
                onChange={(e) => setDeliveryAddress(e.target.value)}
              />
            </label>
          )}
        </div>

        <div className="form-footer-row">
          {!order && (
            <button
              type="button"
              className="btn-secondary"
              onClick={() => setIsCreating(false)}
            >
              Отмена
            </button>
          )}
          {savedToast && <span className="save-toast">✅ Заказ сохранён!</span>}
          <button type="submit" className="btn-primary" disabled={saving}>
            {saving
              ? 'Сохранение...'
              : order
              ? '💾 Сохранить изменения заказа'
              : '✨ Создать активный заказ'}
          </button>
        </div>
      </form>

      {order && (
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
                {uploadingKind === 'reference' ? 'Загрузка...' : '+ Загрузить референс'}
              </button>
            </div>
            {refPhotos.length === 0 ? (
              <div className="photo-empty-hint">
                Нет фото-референсов. Загрузите сюда или нажмите «📌 В референс заказа» на любом
                фото в чате.
              </div>
            ) : (
              <div className="photo-grid">
                {refPhotos.map((p) => (
                  <div key={p.id} className="photo-thumb-card">
                    <img
                      src={p.url}
                      alt={p.caption || 'Референс'}
                      onClick={() => onPreviewImage(p.url, p.caption || 'Референс клиента')}
                    />
                    <button
                      type="button"
                      className="photo-delete-btn"
                      onClick={() => onDeletePhoto(order.id, p.id)}
                      title="Удалить фото"
                    >
                      ✕
                    </button>
                  </div>
                ))}
              </div>
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
                {uploadingKind === 'result' ? 'Загрузка...' : '+ Фото готового торта'}
              </button>
            </div>
            {resPhotos.length === 0 ? (
              <div className="photo-empty-hint">
                Сфотографируйте готовый торт и загрузите сюда — он сохранится в S3 и останется в
                истории заказов клиента.
              </div>
            ) : (
              <div className="photo-grid">
                {resPhotos.map((p) => (
                  <div key={p.id} className="photo-thumb-card">
                    <img
                      src={p.url}
                      alt={p.caption || 'Готовый торт'}
                      onClick={() => onPreviewImage(p.url, p.caption || 'Готовый торт')}
                    />
                    <button
                      type="button"
                      className="photo-delete-btn"
                      onClick={() => onDeletePhoto(order.id, p.id)}
                      title="Удалить фото"
                    >
                      ✕
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

