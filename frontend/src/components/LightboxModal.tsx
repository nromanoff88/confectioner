import React from 'react';

interface LightboxModalProps {
  url: string | null;
  caption?: string;
  onClose: () => void;
}

export const LightboxModal: React.FC<LightboxModalProps> = ({ url, caption, onClose }) => {
  if (!url) return null;

  return (
    <div className="lightbox-backdrop" onClick={onClose}>
      <div className="lightbox-content" onClick={(e) => e.stopPropagation()}>
        <button className="lightbox-close" onClick={onClose} title="Закрыть">
          ✕
        </button>
        <img src={url} alt={caption || 'Фото торта'} className="lightbox-img" />
        {caption && <div className="lightbox-caption">{caption}</div>}
      </div>
    </div>
  );
};

