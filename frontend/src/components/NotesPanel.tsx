import React, { useState } from 'react';
import { Note } from '../types';

interface NotesPanelProps {
  notes: Note[];
  onAdd: (text: string) => Promise<void>;
  onDelete: (noteId: number) => Promise<void>;
}

export const NotesPanel: React.FC<NotesPanelProps> = ({ notes, onAdd, onDelete }) => {
  const [text, setText] = useState('');
  const [saving, setSaving] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!text.trim() || saving) return;
    setSaving(true);
    try {
      await onAdd(text.trim());
      setText('');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="crm-section-card">
      <div className="section-title-row">
        <h3>📝 Журнал заметок</h3>
      </div>
      <form className="notes-form" onSubmit={submit}>
        <textarea
          rows={2}
          placeholder="Например: созвонились, согласовали начинку и время доставки..."
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <button type="submit" className="btn-primary" disabled={saving || !text.trim()}>
          Добавить
        </button>
      </form>

      {notes.length === 0 ? (
        <div className="photo-empty-hint">Заметок пока нет. Записывайте всё, о чём договорились с клиентом.</div>
      ) : (
        <div className="notes-list">
          {notes.map((n) => (
            <div key={n.id} className="note-item">
              <div className="note-meta">
                <span>
                  {n.author_name ? `${n.author_name} • ` : ''}
                  {new Date(n.created_at).toLocaleString('ru-RU', {
                    day: '2-digit',
                    month: '2-digit',
                    year: '2-digit',
                    hour: '2-digit',
                    minute: '2-digit',
                  })}
                </span>
                <button
                  type="button"
                  className="note-delete"
                  title="Удалить заметку"
                  onClick={() => onDelete(n.id)}
                >
                  ✕
                </button>
              </div>
              <div className="note-text">{n.text}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

