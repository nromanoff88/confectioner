import React, { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import { memberDisplayName, TeamInfo } from '../types';

export const TeamView: React.FC = () => {
  const [team, setTeam] = useState<TeamInfo | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    try {
      setTeam(await api.getTeam());
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Ошибка загрузки');
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const copy = async () => {
    if (!team?.invite_link) return;
    try {
      await navigator.clipboard.writeText(team.invite_link);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      window.prompt('Скопируйте ссылку:', team.invite_link);
    }
  };

  const remove = async (userId: number, name: string) => {
    if (!window.confirm(`Убрать «${name}» из команды? Он потеряет доступ к базе клиентов.`)) return;
    try {
      await api.removeMember(userId);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Ошибка');
    }
  };

  if (!team) {
    return <div className="page-empty">{error || 'Загрузка...'}</div>;
  }

  return (
    <div className="page-body">
      <div className="crm-section-card">
        <h3>👥 Команда — {team.workspace_name}</h3>
        <p className="muted">
          Все кондитеры команды видят общую базу клиентов, заказов и фото. Данные других кондитерских вам недоступны.
        </p>
        {error && <div className="form-error">{error}</div>}

        <div className="team-list">
          {team.members.map((m) => (
            <div key={m.max_user_id} className="team-row">
              <div className="chat-avatar">
                <span>{memberDisplayName(m).slice(0, 2).toUpperCase()}</span>
              </div>
              <div className="team-name">
                {memberDisplayName(m)}
                {m.max_user_id === team.me && <em> (вы)</em>}
                <small>{m.role === 'owner' ? 'Владелец' : 'Кондитер'}</small>
              </div>
              {team.is_owner && m.role !== 'owner' && (
                <button
                  type="button"
                  className="btn-danger-outline"
                  onClick={() => remove(m.max_user_id, memberDisplayName(m))}
                >
                  Убрать
                </button>
              )}
            </div>
          ))}
        </div>
      </div>

      {team.is_owner && team.invite_link && (
        <div className="crm-section-card">
          <h3>➕ Пригласить кондитера</h3>
          <p className="muted">
            Отправьте эту ссылку помощнику. Он откроет её в MAX, нажмёт «Старт» у бота — и попадёт в вашу команду.
          </p>
          <div className="invite-box">
            <code>{team.invite_link}</code>
            <button type="button" className="btn-primary" onClick={copy}>
              {copied ? '✅ Скопировано' : 'Копировать'}
            </button>
          </div>
          <p className="muted">
            Либо пусть напишет боту: <code>/join {team.invite_code}</code>
          </p>
        </div>
      )}
    </div>
  );
};

