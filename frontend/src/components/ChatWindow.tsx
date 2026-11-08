import React, { useEffect, useRef, useState } from 'react';
import { Attachment, Client, clientDisplayName, Message, Order } from '../types';

interface ChatWindowProps {
  client: Client;
  messages: Message[];
  activeOrder: Order | null;
  devMode: boolean;
  onBackMobile: () => void;
  onOpenDetailsMobile: () => void;
  onSendMessage: (text: string, file?: File | null) => Promise<void>;
  onSimulateIncoming: () => Promise<void>;
  onAttachPhotoToOrder: (att: Attachment, kind: 'reference' | 'result') => Promise<void>;
  onPreviewImage: (url: string, caption?: string) => void;
}

export const ChatWindow: React.FC<ChatWindowProps> = ({
  client,
  messages,
  activeOrder,
  devMode,
  onBackMobile,
  onOpenDetailsMobile,
  onSendMessage,
  onSimulateIncoming,
  onAttachPhotoToOrder,
  onPreviewImage,
}) => {
  const [text, setText] = useState('');
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [sending, setSending] = useState(false);
  const [attachingKey, setAttachingKey] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const messagesEndRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length, client.id]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if ((!text.trim() && !selectedFile) || sending) return;
    setSending(true);
    try {
      await onSendMessage(text.trim(), selectedFile);
      setText('');
      setSelectedFile(null);
      if (fileInputRef.current) fileInputRef.current.value = '';
    } finally {
      setSending(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void handleSubmit(e);
    }
  };

  const handleAttach = async (att: Attachment, kind: 'reference' | 'result') => {
    const key = `${att.url}_${kind}`;
    setAttachingKey(key);
    try {
      await onAttachPhotoToOrder(att, kind);
    } finally {
      setAttachingKey(null);
    }
  };

  const name = clientDisplayName(client);

  return (
    <div className="chat-window">
      <div className="chat-window-header">
        <div className="chat-header-left">
          <button className="btn-back-mobile" onClick={onBackMobile}>
            ← Чаты
          </button>
          <div className="chat-header-avatar">
            {name.slice(0, 2).toUpperCase()}
          </div>
          <div className="chat-header-info" onClick={onOpenDetailsMobile}>
            <h2>{name}</h2>
            <div className="chat-header-meta">
              {client.phone ? <span>📞 {client.phone}</span> : <span>Телефон не указан</span>}
              {client.address && <span> • 📍 {client.address}</span>}
            </div>
          </div>
        </div>

        <div className="chat-header-actions">
          {devMode && (
            <button
              type="button"
              className="btn-sim"
              onClick={onSimulateIncoming}
              title="Симулировать входящее сообщение от клиента для проверки"
            >
              ⚡ Тест-сообщение
            </button>
          )}
          <button
            type="button"
            className="btn-open-card-mobile"
            onClick={onOpenDetailsMobile}
          >
            👤 Карточка и Заказы {activeOrder ? '🎂' : ''}
          </button>
        </div>
      </div>

      {client.description && (
        <div className="client-quick-note">
          <strong>📝 Заметка о клиенте:</strong> {client.description}
        </div>
      )}

      <div className="chat-messages-area">
        {messages.length === 0 ? (
          <div className="chat-empty-state">
            <p>Сообщений пока нет</p>
            <small>Напишите клиенту — сообщение придёт ему от имени бота в MAX.</small>
          </div>
        ) : (
          messages.map((m) => {
            if (m.sender_type === 'system') {
              return (
                <div key={m.id} className="msg-system">
                  <span>{m.text}</span>
                </div>
              );
            }

            const isOperator = m.sender_type === 'operator';
            const timeStr = new Date(m.created_at).toLocaleTimeString('ru-RU', {
              hour: '2-digit',
              minute: '2-digit',
            });

            return (
              <div
                key={m.id}
                className={`msg-row ${isOperator ? 'msg-operator' : 'msg-client'}`}
              >
                <div className="msg-bubble">
                  <div className="msg-sender-label">
                    {isOperator ? '👩‍🍳 Вы (от имени бота)' : `👤 ${name}`}
                  </div>

                  {m.attachments && m.attachments.length > 0 && (
                    <div className="msg-attachments">
                      {m.attachments.map((att, idx) => (
                        <div key={idx} className="msg-photo-card">
                          <img
                            src={att.url}
                            alt={att.filename || 'Фото'}
                            onClick={() => onPreviewImage(att.url, m.text || 'Фото из чата')}
                          />
                          <div className="msg-photo-actions">
                            <button
                              type="button"
                              disabled={attachingKey === `${att.url}_reference`}
                              onClick={() => handleAttach(att, 'reference')}
                              title="Прикрепить это фото как референс к активному заказу"
                            >
                              📌 В референс заказа
                            </button>
                            <button
                              type="button"
                              disabled={attachingKey === `${att.url}_result`}
                              onClick={() => handleAttach(att, 'result')}
                              title="Прикрепить это фото как фото готового торта"
                            >
                              🎂 В фото готового торта
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}

                  {m.text && <div className="msg-text">{m.text}</div>}
                  <div className="msg-time">{timeStr}</div>
                </div>
              </div>
            );
          })
        )}
        <div ref={messagesEndRef} />
      </div>

      <form className="chat-composer" onSubmit={handleSubmit}>
        {selectedFile && (
          <div className="composer-file-chip">
            <span>📷 {selectedFile.name}</span>
            <button
              type="button"
              onClick={() => {
                setSelectedFile(null);
                if (fileInputRef.current) fileInputRef.current.value = '';
              }}
            >
              ✕
            </button>
          </div>
        )}

        <div className="composer-row">
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            style={{ display: 'none' }}
            onChange={(e) => {
              const f = e.target.files?.[0] || null;
              setSelectedFile(f);
            }}
          />
          <button
            type="button"
            className="btn-attach-photo"
            onClick={() => fileInputRef.current?.click()}
            title="Прикрепить фотографию торта"
          >
            📷
          </button>

          <textarea
            rows={1}
            placeholder={`Ответить клиенту ${name} от имени бота...`}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
          />

          <button
            type="submit"
            className="btn-send"
            disabled={sending || (!text.trim() && !selectedFile)}
          >
            {sending ? '...' : 'Отправить ➤'}
          </button>
        </div>
      </form>
    </div>
  );
};

