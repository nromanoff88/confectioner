import React, { useCallback, useEffect, useRef, useState } from 'react';
import { api } from './api/client';
import { getStartParam, triggerHapticImpact, triggerHapticSuccess } from './api/bridge';
import {
  Client,
  ClientDetails,
  ClientInput,
  clientDisplayName,
  Order,
  OrderInput,
  OrderStatus,
} from './types';
import { ClientList } from './components/ClientList';
import { ClientCard } from './components/ClientCard';
import { NotesPanel } from './components/NotesPanel';
import { ActiveOrderPanel } from './components/ActiveOrderPanel';
import { OrderHistoryPanel } from './components/OrderHistoryPanel';
import { CalendarView } from './components/CalendarView';
import { StatsView } from './components/StatsView';
import { TeamView } from './components/TeamView';
import { LightboxModal } from './components/LightboxModal';

type Tab = 'clients' | 'calendar' | 'stats' | 'team';
type DetailTab = 'order' | 'profile' | 'notes' | 'history';

export const App: React.FC = () => {
  const [workspaceName, setWorkspaceName] = useState('');
  const [authError, setAuthError] = useState('');
  const [ready, setReady] = useState(false);

  const [tab, setTab] = useState<Tab>('clients');
  const [clients, setClients] = useState<Client[]>([]);
  const [allTags, setAllTags] = useState<string[]>([]);
  const [search, setSearch] = useState('');
  const [tag, setTag] = useState('');

  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [details, setDetails] = useState<ClientDetails | null>(null);
  const [detailTab, setDetailTab] = useState<DetailTab>('order');
  const [mobileView, setMobileView] = useState<'list' | 'details'>('list');

  const [activeOrders, setActiveOrders] = useState<Order[]>([]);
  const [ordersLoading, setOrdersLoading] = useState(false);

  const [openNewClient, setOpenNewClient] = useState(false);
  const [startCreatingOrder, setStartCreatingOrder] = useState(false);
  const pendingNewOrder = useRef(false);

  const [toast, setToast] = useState<string | null>(null);
  const [lightbox, setLightbox] = useState<{ url: string; caption?: string } | null>(null);

  const showToast = useCallback((msg: string) => {
    setToast(msg);
    setTimeout(() => setToast((prev) => (prev === msg ? null : prev)), 3200);
  }, []);

  const loadClients = useCallback(async (q: string, t: string) => {
    try {
      const list = await api.listClients(q, t);
      setClients(list);
      if (!q && !t) {
        setAllTags(Array.from(new Set(list.flatMap((c) => c.tags))).sort((a, b) => a.localeCompare(b, 'ru')));
      }
    } catch (err) {
      console.error('loadClients error:', err);
    }
  }, []);

  const loadDetails = useCallback(async (id: number) => {
    try {
      setDetails(await api.getClientDetails(id));
    } catch (err) {
      console.error('loadDetails error:', err);
      setDetails(null);
    }
  }, []);

  const loadActiveOrders = useCallback(async () => {
    setOrdersLoading(true);
    try {
      setActiveOrders(await api.listActiveOrders());
    } catch (err) {
      console.error('loadActiveOrders error:', err);
    } finally {
      setOrdersLoading(false);
    }
  }, []);

  const selectClient = useCallback(
    (id: number, preferTab?: DetailTab) => {
      setSelectedId(id);
      setDetailTab(preferTab || 'order');
      setMobileView('details');
      if (pendingNewOrder.current) {
        pendingNewOrder.current = false;
        setStartCreatingOrder(true);
      }
      void loadDetails(id);
    },
    [loadDetails]
  );

  // Initial load: identify the confectioner, then route the deep link (startapp=...)
  useEffect(() => {
    void (async () => {
      try {
        const me = await api.getMe();
        setWorkspaceName(me.workspace_name);
      } catch (err) {
        setAuthError(err instanceof Error ? err.message : 'Ошибка авторизации');
        setReady(true);
        return;
      }
      await loadClients('', '');
      setReady(true);

      const start = getStartParam();
      if (start === 'new_client') {
        setOpenNewClient(true);
      } else if (start === 'new_order') {
        pendingNewOrder.current = true;
        showToast('Выберите клиента (или запишите нового), чтобы создать заказ');
      } else if (start === 'orders_today') {
        setTab('calendar');
      } else if (start.startsWith('client_')) {
        const id = parseInt(start.replace('client_', ''), 10);
        if (!isNaN(id)) selectClient(id);
      }
    })();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!ready || authError) return;
    const timer = setTimeout(() => void loadClients(search, tag), 180);
    return () => clearTimeout(timer);
  }, [search, tag, ready, authError, loadClients]);

  useEffect(() => {
    if (tab === 'calendar') void loadActiveOrders();
  }, [tab, loadActiveOrders]);

  // MAX Bridge back button: details -> list
  useEffect(() => {
    const bb = window.WebApp?.BackButton;
    if (!bb) return;
    const handleBack = () => setMobileView('list');
    if (tab === 'clients' && mobileView === 'details') {
      bb.show();
      bb.onClick(handleBack);
    } else {
      bb.hide();
    }
    return () => bb.offClick(handleBack);
  }, [tab, mobileView]);

  const refresh = useCallback(
    async (id: number | null = selectedId) => {
      await loadClients(search, tag);
      if (id) await loadDetails(id);
    },
    [selectedId, search, tag, loadClients, loadDetails]
  );

  // ----- Client actions -----

  const handleCreateClient = async (data: ClientInput) => {
    const created = await api.createClient(data);
    await loadClients(search, tag);
    triggerHapticSuccess();
    showToast(`✅ Клиент «${clientDisplayName(created)}» записан`);
    selectClient(created.id, 'order');
  };

  const handleSaveClient = async (data: ClientInput) => {
    if (!selectedId) return;
    await api.updateClient(selectedId, data);
    await refresh();
    triggerHapticSuccess();
  };

  const handleDeleteClient = async () => {
    if (!selectedId) return;
    await api.deleteClient(selectedId);
    setSelectedId(null);
    setDetails(null);
    setMobileView('list');
    await loadClients(search, tag);
    showToast('Клиент удалён');
  };

  const handleAddNote = async (text: string) => {
    if (!selectedId) return;
    await api.addNote(selectedId, text);
    await loadDetails(selectedId);
  };

  const handleDeleteNote = async (noteId: number) => {
    if (!selectedId) return;
    await api.deleteNote(noteId);
    await loadDetails(selectedId);
  };

  // ----- Order actions -----

  const handleSaveOrder = async (payload: OrderInput) => {
    if (!selectedId) return;
    await api.saveOrder(selectedId, payload);
    await refresh();
    triggerHapticSuccess();
  };

  const handleChangeStatus = async (orderId: number, status: OrderStatus) => {
    if (!selectedId) return;
    await api.updateOrderStatus(orderId, status);
    await refresh();
    triggerHapticSuccess();
    if (status === 'completed' || status === 'cancelled') {
      setDetailTab('history');
      showToast('Заказ перенесён в историю клиента');
    }
  };

  const handleDeleteOrder = async (orderId: number) => {
    if (!selectedId) return;
    await api.deleteOrder(orderId);
    await refresh();
    showToast('Заказ удалён');
  };

  const handleUploadPhoto = async (orderId: number, kind: 'reference' | 'result', file: File) => {
    if (!selectedId) return;
    await api.uploadOrderPhoto(orderId, kind, file);
    await loadDetails(selectedId);
    triggerHapticSuccess();
    showToast('📷 Фото сохранено');
  };

  const handleDeletePhoto = async (orderId: number, photoId: number) => {
    if (!selectedId) return;
    await api.deleteOrderPhoto(orderId, photoId);
    await loadDetails(selectedId);
  };

  const openClientFromOtherTab = (clientId: number) => {
    setTab('clients');
    selectClient(clientId);
  };

  // ----- Render -----

  if (!ready) {
    return <div className="page-empty">Загрузка...</div>;
  }
  if (authError) {
    return (
      <div className="page-empty">
        <div className="empty-order-icon">🔒</div>
        <h3>Нет доступа</h3>
        <p>{authError}</p>
      </div>
    );
  }

  const selected = details?.client || null;

  return (
    <div className={`app-shell view-${mobileView}`}>
      <header className="topbar">
        <div className="topbar-title">🎂 {workspaceName || 'Кондитерская'}</div>
        <nav className="top-tabs">
          {(
            [
              ['clients', '👥 Клиенты'],
              ['calendar', '📅 Календарь'],
              ['stats', '📊 Статистика'],
              ['team', '🤝 Команда'],
            ] as [Tab, string][]
          ).map(([key, label]) => (
            <button
              key={key}
              className={tab === key ? 'active' : ''}
              onClick={() => {
                triggerHapticImpact('light');
                setTab(key);
              }}
            >
              {label}
            </button>
          ))}
        </nav>
      </header>

      {toast && <div className="floating-toast">{toast}</div>}

      <main className="app-main">
        {tab === 'clients' && (
          <div className="crm-grid">
            <div className="crm-col-list">
              <ClientList
                clients={clients}
                selectedClientId={selectedId}
                search={search}
                tag={tag}
                allTags={allTags}
                autoOpenNew={openNewClient}
                onAutoOpenHandled={() => setOpenNewClient(false)}
                onSearchChange={setSearch}
                onTagChange={setTag}
                onSelectClient={(c) => selectClient(c.id)}
                onCreateClient={handleCreateClient}
              />
            </div>

            <div className="crm-col-details">
              {selected && details ? (
                <div className="details-container">
                  <div className="details-mobile-bar">
                    <button type="button" className="btn-back-mobile" onClick={() => setMobileView('list')}>
                      ← Клиенты
                    </button>
                    <span className="details-mobile-title">{clientDisplayName(selected)}</span>
                  </div>

                  <div className="details-head">
                    <h2>{clientDisplayName(selected)}</h2>
                    {selected.tags.length > 0 && (
                      <div className="tag-row small">
                        {selected.tags.map((t) => (
                          <span key={t} className="tag-chip static">
                            {t}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="details-tabs">
                    <button
                      className={detailTab === 'order' ? 'active' : ''}
                      onClick={() => setDetailTab('order')}
                    >
                      🎂 Заказ{details.active_order ? ' •' : ''}
                    </button>
                    <button
                      className={detailTab === 'profile' ? 'active' : ''}
                      onClick={() => setDetailTab('profile')}
                    >
                      👤 Клиент
                    </button>
                    <button
                      className={detailTab === 'notes' ? 'active' : ''}
                      onClick={() => setDetailTab('notes')}
                    >
                      📝 Заметки ({details.notes.length})
                    </button>
                    <button
                      className={detailTab === 'history' ? 'active' : ''}
                      onClick={() => setDetailTab('history')}
                    >
                      📚 История ({details.history.length})
                    </button>
                  </div>

                  <div className="details-scroll-body">
                    {detailTab === 'order' && (
                      <ActiveOrderPanel
                        client={selected}
                        order={details.active_order}
                        startCreating={startCreatingOrder}
                        onCreatingHandled={() => setStartCreatingOrder(false)}
                        onSaveOrder={handleSaveOrder}
                        onChangeStatus={handleChangeStatus}
                        onDeleteOrder={handleDeleteOrder}
                        onUploadPhoto={handleUploadPhoto}
                        onDeletePhoto={handleDeletePhoto}
                        onPreviewImage={(url, caption) => setLightbox({ url, caption })}
                      />
                    )}
                    {detailTab === 'profile' && (
                      <ClientCard client={selected} onSave={handleSaveClient} onDelete={handleDeleteClient} />
                    )}
                    {detailTab === 'notes' && (
                      <NotesPanel notes={details.notes} onAdd={handleAddNote} onDelete={handleDeleteNote} />
                    )}
                    {detailTab === 'history' && (
                      <OrderHistoryPanel
                        history={details.history}
                        onUploadHistoryPhoto={handleUploadPhoto}
                        onPreviewImage={(url, caption) => setLightbox({ url, caption })}
                      />
                    )}
                  </div>
                </div>
              ) : (
                <div className="no-details-selected">
                  <div className="no-chat-icon">🎂</div>
                  <h2>Выберите клиента</h2>
                  <p>Или запишите нового кнопкой «+ Записать клиента».</p>
                </div>
              )}
            </div>
          </div>
        )}

        {tab === 'calendar' && (
          <CalendarView orders={activeOrders} loading={ordersLoading} onOpenClient={openClientFromOtherTab} />
        )}
        {tab === 'stats' && <StatsView onOpenClient={openClientFromOtherTab} />}
        {tab === 'team' && <TeamView />}
      </main>

      <LightboxModal url={lightbox?.url || null} caption={lightbox?.caption} onClose={() => setLightbox(null)} />
    </div>
  );
};
