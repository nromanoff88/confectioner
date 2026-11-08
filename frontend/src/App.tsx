import React, { useCallback, useEffect, useRef, useState } from 'react';
import { api } from './api/client';
import {
  getMaxInitData,
  getStartParam,
  triggerHapticImpact,
  triggerHapticSuccess,
} from './api/bridge';
import {
  Attachment,
  Client,
  clientDisplayName,
  DeliveryType,
  Message,
  Order,
  OrderStatus,
  PaymentStatus,
} from './types';
import { ChatList } from './components/ChatList';
import { ChatWindow } from './components/ChatWindow';
import { ClientCard } from './components/ClientCard';
import { ActiveOrderPanel } from './components/ActiveOrderPanel';
import { OrderHistoryPanel } from './components/OrderHistoryPanel';
import { LightboxModal } from './components/LightboxModal';

type MobileScreen = 'list' | 'chat' | 'details';
type DetailsTab = 'active_order' | 'profile' | 'history';

export const App: React.FC = () => {
  const [clients, setClients] = useState<Client[]>([]);
  const [selectedClientId, setSelectedClientId] = useState<number | null>(null);
  const [selectedClient, setSelectedClient] = useState<Client | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [activeOrder, setActiveOrder] = useState<Order | null>(null);
  const [orderHistory, setOrderHistory] = useState<Order[]>([]);

  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<'all' | 'unread' | 'active_order'>('all');
  const [mobileScreen, setMobileScreen] = useState<MobileScreen>('list');
  const [detailsTab, setDetailsTab] = useState<DetailsTab>('active_order');

  const [devMode, setDevMode] = useState(true);
  const [botConfigured, setBotConfigured] = useState(false);
  const [toastMsg, setToastMsg] = useState<string | null>(null);
  const [lightbox, setLightbox] = useState<{ url: string; caption?: string } | null>(null);

  const selectedIdRef = useRef<number | null>(null);
  selectedIdRef.current = selectedClientId;

  const showToast = useCallback((msg: string) => {
    setToastMsg(msg);
    setTimeout(() => {
      setToastMsg((prev) => (prev === msg ? null : prev));
    }, 3200);
  }, []);

  const loadClients = useCallback(async (q = search, f = filter) => {
    try {
      const list = await api.listClients(q, f);
      setClients(list);
      return list;
    } catch (err) {
      console.error('loadClients error:', err);
      return [];
    }
  }, [search, filter]);

  const loadClientFull = useCallback(async (clientId: number) => {
    try {
      const [details, msgs] = await Promise.all([
        api.getClientDetails(clientId),
        api.listMessages(clientId),
      ]);
      setSelectedClient(details.client);
      setActiveOrder(details.active_order || null);
      setOrderHistory(details.history || []);
      setMessages(msgs);
    } catch (err) {
      console.error('loadClientFull error:', err);
    }
  }, []);

  // Initial load + deep link handling (startapp=client_123)
  useEffect(() => {
    void (async () => {
      try {
        const health = await api.getHealth();
        setDevMode(health.dev_mode);
        setBotConfigured(health.bot_configured);
      } catch {
        // ignore
      }

      const list = await loadClients('', 'all');
      const startParam = getStartParam();
      if (startParam.startsWith('client_')) {
        const targetId = parseInt(startParam.replace('client_', ''), 10);
        if (!isNaN(targetId)) {
          setSelectedClientId(targetId);
          setMobileScreen('chat');
          void loadClientFull(targetId);
          return;
        }
      }
      if (list.length > 0 && window.innerWidth >= 960) {
        setSelectedClientId(list[0].id);
        void loadClientFull(list[0].id);
      }
    })();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Reload client list when search or filter changes
  useEffect(() => {
    const timer = setTimeout(() => {
      void loadClients(search, filter);
    }, 180);
    return () => clearTimeout(timer);
  }, [search, filter, loadClients]);

  // MAX Bridge BackButton integration on mobile screens
  useEffect(() => {
    const bb = window.WebApp?.BackButton;
    if (!bb) return;

    const handleBack = () => {
      if (mobileScreen === 'details') {
        setMobileScreen('chat');
      } else if (mobileScreen === 'chat') {
        setMobileScreen('list');
      }
    };

    if (mobileScreen !== 'list') {
      bb.show();
      bb.onClick(handleBack);
    } else {
      bb.hide();
    }

    return () => {
      bb.offClick(handleBack);
    };
  }, [mobileScreen]);

  // Real-time Server-Sent Events (SSE) subscription
  useEffect(() => {
    const initData = getMaxInitData();
    const url = initData
      ? `/api/events?init_data=${encodeURIComponent(initData)}`
      : '/api/events';

    const es = new EventSource(url);
    es.onmessage = (evt) => {
      try {
        const parsed = JSON.parse(evt.data);
        if (parsed.type === 'new_message') {
          const { client, message } = parsed.payload as {
            client: Client;
            message: Message;
          };
          void loadClients();
          if (selectedIdRef.current === client.id) {
            setSelectedClient(client);
            setMessages((prev) => {
              if (prev.some((m) => m.id === message.id)) return prev;
              return [...prev, message];
            });
          } else if (message.sender_type === 'client') {
            showToast(`📩 Новое сообщение от ${clientDisplayName(client)}`);
          }
        } else if (parsed.type === 'client_updated') {
          const updated = parsed.payload as Client;
          void loadClients();
          if (selectedIdRef.current === updated.id) {
            setSelectedClient(updated);
          }
        } else if (parsed.type === 'order_updated') {
          const ord = parsed.payload as Order;
          void loadClients();
          if (selectedIdRef.current === ord.client_id) {
            void loadClientFull(ord.client_id);
          }
        }
      } catch {
        // ignore ping or malformed events
      }
    };

    return () => {
      es.close();
    };
  }, [loadClients, loadClientFull, showToast]);

  const handleSelectClient = (client: Client) => {
    triggerHapticImpact('light');
    setSelectedClientId(client.id);
    setSelectedClient(client);
    setMobileScreen('chat');
    void loadClientFull(client.id);
  };

  const handleCreateClient = async (data: {
    custom_name: string;
    phone: string;
    address: string;
    description: string;
  }) => {
    const created = await api.createClient(data);
    await loadClients();
    setSelectedClientId(created.id);
    setSelectedClient(created);
    setMobileScreen('chat');
    await loadClientFull(created.id);
    triggerHapticSuccess();
    showToast(`✅ Клиент ${clientDisplayName(created)} создан`);
  };

  const handleSendMessage = async (text: string, file?: File | null) => {
    if (!selectedClientId) return;
    const msg = await api.sendMessage(selectedClientId, text, file);
    setMessages((prev) => (prev.some((m) => m.id === msg.id) ? prev : [...prev, msg]));
    triggerHapticImpact('soft');
    void loadClients();
  };

  const handleSimulateIncoming = async () => {
    if (!selectedClientId) return;
    await api.simulateClientMessage(
      selectedClientId,
      'Подскажите, пожалуйста, а можно сделать надпись золотым шоколадом? ✨'
    );
  };

  const handleAttachPhotoToOrder = async (
    att: Attachment,
    kind: 'reference' | 'result'
  ) => {
    if (!selectedClientId || !selectedClient) return;
    let targetOrder = activeOrder;

    // If the client doesn't have an active order yet, create one automatically!
    if (!targetOrder) {
      targetOrder = await api.saveOrder(selectedClientId, {
        status: 'new',
        cake_description: 'Новый заказ (создан из фото в чате)',
        filling: '',
        decor_inscription: '',
        weight_kg: 2.0,
        price: 0,
        prepayment: 0,
        payment_status: 'unpaid',
        delivery_type: selectedClient.address ? 'delivery' : 'pickup',
        delivery_address: selectedClient.address || '',
        delivery_date: '',
      });
    }

    await api.attachChatPhotoToOrder(
      targetOrder.id,
      kind,
      att.s3_key,
      att.url,
      kind === 'reference' ? 'Референс из чата' : 'Готовый торт из чата'
    );
    await loadClientFull(selectedClientId);
    triggerHapticSuccess();
    showToast(
      kind === 'reference'
        ? '📌 Фото прикреплено в референсы заказа!'
        : '🎂 Фото прикреплено как готовый торт!'
    );
  };

  const handleSaveProfile = async (data: {
    custom_name: string;
    phone: string;
    address: string;
    description: string;
  }) => {
    if (!selectedClientId) return;
    const updated = await api.updateClientProfile(selectedClientId, data);
    setSelectedClient(updated);
    triggerHapticSuccess();
    void loadClients();
  };

  const handleSaveOrder = async (payload: {
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
  }) => {
    if (!selectedClientId) return;
    await api.saveOrder(selectedClientId, payload);
    await loadClientFull(selectedClientId);
    await loadClients();
    triggerHapticSuccess();
    showToast('✅ Заказ сохранён');
  };

  const handleChangeOrderStatus = async (orderId: number, status: OrderStatus) => {
    if (!selectedClientId) return;
    await api.updateOrderStatus(orderId, status);
    await loadClientFull(selectedClientId);
    await loadClients();
    triggerHapticSuccess();
    if (status === 'completed' || status === 'cancelled') {
      setDetailsTab('history');
      showToast('🔔 Статус обновлён и заказ перенесён в историю!');
    } else {
      showToast('🔔 Статус обновлён, клиенту отправлено уведомление!');
    }
  };

  const handleUploadOrderPhoto = async (
    orderId: number,
    kind: 'reference' | 'result',
    file: File
  ) => {
    if (!selectedClientId) return;
    await api.uploadOrderPhoto(orderId, kind, file);
    await loadClientFull(selectedClientId);
    triggerHapticSuccess();
    showToast('📷 Фотография загружена в S3 и прикреплена к заказу!');
  };

  const handleDeleteOrderPhoto = async (orderId: number, photoId: number) => {
    if (!selectedClientId) return;
    await api.deleteOrderPhoto(orderId, photoId);
    await loadClientFull(selectedClientId);
    showToast('Фото удалено из заказа');
  };

  return (
    <div className={`app-shell mobile-screen-${mobileScreen}`}>
      {!botConfigured && (
        <div className="top-status-banner">
          <span>
            ℹ️ <strong>Демо/Локальный режим:</strong> Укажите <code>MAX_BOT_TOKEN</code> в файле{' '}
            <code>.env</code>, чтобы подключить реального бота MAX (<code>platform-api2.max.ru</code>).
          </span>
        </div>
      )}

      {toastMsg && <div className="floating-toast">{toastMsg}</div>}

      <div className="workspace-grid">
        {/* LEFT COLUMN: Client Chats List */}
        <div className="col-list">
          <ChatList
            clients={clients}
            selectedClientId={selectedClientId}
            search={search}
            filter={filter}
            onSearchChange={setSearch}
            onFilterChange={setFilter}
            onSelectClient={handleSelectClient}
            onCreateClient={handleCreateClient}
          />
        </div>

        {/* CENTER COLUMN: Active Chat Window */}
        <div className="col-chat">
          {selectedClient ? (
            <ChatWindow
              client={selectedClient}
              messages={messages}
              activeOrder={activeOrder}
              devMode={devMode}
              onBackMobile={() => setMobileScreen('list')}
              onOpenDetailsMobile={() => setMobileScreen('details')}
              onSendMessage={handleSendMessage}
              onSimulateIncoming={handleSimulateIncoming}
              onAttachPhotoToOrder={handleAttachPhotoToOrder}
              onPreviewImage={(url, caption) => setLightbox({ url, caption })}
            />
          ) : (
            <div className="no-chat-selected">
              <div className="no-chat-icon">🎂</div>
              <h2>Выберите диалог слева</h2>
              <p>
                Здесь отобразится переписка с клиентом, его карточка, активный заказ и фотографии
                тортов из истории заказов.
              </p>
            </div>
          )}
        </div>

        {/* RIGHT COLUMN: Client Card, Active Order, Order History */}
        <div className="col-details">
          {selectedClient ? (
            <div className="details-container">
              <div className="details-mobile-bar">
                <button
                  type="button"
                  className="btn-back-mobile"
                  onClick={() => setMobileScreen('chat')}
                >
                  ← Вернуться в чат
                </button>
                <span className="details-mobile-title">{clientDisplayName(selectedClient)}</span>
              </div>

              <div className="details-tabs">
                <button
                  type="button"
                  className={detailsTab === 'active_order' ? 'active' : ''}
                  onClick={() => setDetailsTab('active_order')}
                >
                  🎂 Активный заказ {activeOrder ? '• 1' : ''}
                </button>
                <button
                  type="button"
                  className={detailsTab === 'profile' ? 'active' : ''}
                  onClick={() => setDetailsTab('profile')}
                >
                  👤 Клиент
                </button>
                <button
                  type="button"
                  className={detailsTab === 'history' ? 'active' : ''}
                  onClick={() => setDetailsTab('history')}
                >
                  📚 История ({orderHistory.length})
                </button>
              </div>

              <div className="details-scroll-body">
                {detailsTab === 'active_order' && (
                  <ActiveOrderPanel
                    client={selectedClient}
                    order={activeOrder}
                    onSaveOrder={handleSaveOrder}
                    onChangeStatus={handleChangeOrderStatus}
                    onUploadPhoto={handleUploadOrderPhoto}
                    onDeletePhoto={handleDeleteOrderPhoto}
                    onPreviewImage={(url, caption) => setLightbox({ url, caption })}
                  />
                )}

                {detailsTab === 'profile' && (
                  <ClientCard client={selectedClient} onSaveProfile={handleSaveProfile} />
                )}

                {detailsTab === 'history' && (
                  <OrderHistoryPanel
                    history={orderHistory}
                    onUploadHistoryPhoto={handleUploadOrderPhoto}
                    onPreviewImage={(url, caption) => setLightbox({ url, caption })}
                  />
                )}
              </div>
            </div>
          ) : (
            <div className="no-details-selected">
              <p>Выберите клиента для просмотра карточки и заказов</p>
            </div>
          )}
        </div>
      </div>

      <LightboxModal
        url={lightbox?.url || null}
        caption={lightbox?.caption}
        onClose={() => setLightbox(null)}
      />
    </div>
  );
};

