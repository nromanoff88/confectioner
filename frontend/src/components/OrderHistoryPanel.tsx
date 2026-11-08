import React, { useRef, useState } from 'react';
import { Order, ORDER_STATUS_LABELS } from '../types';

interface OrderHistoryPanelProps {
  history: Order[];
  onUploadHistoryPhoto: (
    orderId: number,
    kind: 'reference' | 'result',
    file: File
  ) => Promise<void>;
  onPreviewImage: (url: string, caption?: string) => void;
}

export const OrderHistoryPanel: React.FC<OrderHistoryPanelProps> = ({
  history,
  onUploadHistoryPhoto,
  onPreviewImage,
}) => {
  const [uploadingOrderId, setUploadingOrderId] = useState<number | null>(null);
  const fileInputs = useRef<Record<number, HTMLInputElement | null>>({});

  if (history.length === 0) {
    return (
      <div className="crm-section-card empty-order-box">
        <div className="empty-order-icon">📚</div>
        <h3>История заказов пуста</h3>
        <p>
          Завершённые заказы клиента автоматически попадают сюда вместе с фотографиями тортов,
          весом, начинками и датами.
        </p>
      </div>
    );
  }

  const handleUpload = async (orderId: number, file?: File | null) => {
    if (!file) return;
    setUploadingOrderId(orderId);
    try {
      await onUploadHistoryPhoto(orderId, 'result', file);
    } finally {
      setUploadingOrderId(null);
    }
  };

  return (
    <div className="history-list">
      {history.map((o) => {
        const dateStr = o.delivery_date
          ? new Date(o.delivery_date).toLocaleDateString('ru-RU', {
              day: '2-digit',
              month: 'long',
              year: 'numeric',
            })
          : new Date(o.created_at).toLocaleDateString('ru-RU', {
              day: '2-digit',
              month: 'long',
              year: 'numeric',
            });

        return (
          <div key={o.id} className="crm-section-card history-order-card">
            <div className="history-card-header">
              <div>
                <span className="history-order-num">Заказ #{o.id}</span>
                <span className="history-order-date"> • {dateStr}</span>
              </div>
              <span className={`status-pill status-${o.status}`}>
                {ORDER_STATUS_LABELS[o.status] || o.status}
              </span>
            </div>

            <div className="history-order-title">{o.cake_description || 'Без описания'}</div>

            <div className="history-specs-grid">
              {o.filling && (
                <div>
                  <strong>Начинка:</strong> {o.filling}
                </div>
              )}
              {o.decor_inscription && (
                <div>
                  <strong>Надпись / декор:</strong> {o.decor_inscription}
                </div>
              )}
              {o.weight_kg > 0 && (
                <div>
                  <strong>Вес:</strong> {o.weight_kg} кг
                </div>
              )}
              {o.price > 0 && (
                <div>
                  <strong>Стоимость:</strong> {o.price} ₽
                </div>
              )}
              <div>
                <strong>Получение:</strong>{' '}
                {o.delivery_type === 'delivery'
                  ? `Доставка (${o.delivery_address || 'по адресу'})`
                  : 'Самовывоз'}
              </div>
            </div>

            <div className="history-photos-block">
              <div className="photo-block-header">
                <h4>📷 Фотографии тортов этого заказа ({(o.photos || []).length})</h4>
                <input
                  ref={(el) => {
                    fileInputs.current[o.id] = el;
                  }}
                  type="file"
                  accept="image/*"
                  style={{ display: 'none' }}
                  onChange={(e) => {
                    void handleUpload(o.id, e.target.files?.[0]);
                    if (fileInputs.current[o.id]) fileInputs.current[o.id]!.value = '';
                  }}
                />
                <button
                  type="button"
                  className="btn-small-outline"
                  disabled={uploadingOrderId === o.id}
                  onClick={() => fileInputs.current[o.id]?.click()}
                >
                  {uploadingOrderId === o.id ? 'Загрузка...' : '+ Добавить фото торта'}
                </button>
              </div>

              {(o.photos || []).length === 0 ? (
                <div className="photo-empty-hint">Нет прикреплённых фотографий.</div>
              ) : (
                <div className="photo-grid">
                  {o.photos.map((p) => (
                    <div key={p.id} className="photo-thumb-card">
                      <img
                        src={p.url}
                        alt={p.caption || 'Торт'}
                        onClick={() =>
                          onPreviewImage(
                            p.url,
                            `${o.cake_description} (${dateStr}) — ${
                              p.kind === 'result' ? 'Готовый торт' : 'Референс'
                            }`
                          )
                        }
                      />
                      <span className={`photo-kind-tag kind-${p.kind}`}>
                        {p.kind === 'result' ? '🎂 Торт' : '📌 Референс'}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
};

