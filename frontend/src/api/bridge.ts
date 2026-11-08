export interface MaxWebAppInitDataUser {
  id: number;
  first_name: string;
  last_name: string;
  username: string;
  language_code: string;
  photo_url: string;
}

export interface MaxWebAppInitData {
  query_id: string;
  auth_date: number;
  hash: string;
  user?: MaxWebAppInitDataUser;
  start_param?: string;
}

export interface MaxWebAppBackButton {
  readonly isVisible: boolean;
  show(): void;
  hide(): void;
  onClick(callback: () => void): void;
  offClick(callback: () => void): void;
}

export interface MaxWebAppHapticFeedback {
  impactOccurred(
    impactStyle: 'soft' | 'light' | 'medium' | 'heavy' | 'rigid',
    disableVibrationFallback?: boolean
  ): void;
  notificationOccurred(
    notificationType: 'error' | 'success' | 'warning',
    disableVibrationFallback?: boolean
  ): void;
  selectionChanged(disableVibrationFallback?: boolean): void;
}

export interface MaxWebApp {
  readonly initData: string;
  readonly initDataUnsafe: MaxWebAppInitData;
  readonly platform: 'ios' | 'android' | 'desktop' | 'web';
  readonly version: string;
  readonly deviceName: string;
  readonly BackButton: MaxWebAppBackButton;
  readonly HapticFeedback: MaxWebAppHapticFeedback;
  openLink(url: string): void;
  openMaxLink(url: string): void;
  downloadFile(url: string, file_name: string): Promise<void>;
}

declare global {
  interface Window {
    WebApp?: MaxWebApp;
  }
}

export function getMaxInitData(): string {
  return window.WebApp?.initData || '';
}

export function getStartParam(): string {
  const fromBridge = window.WebApp?.initDataUnsafe?.start_param;
  if (fromBridge) return fromBridge;
  const params = new URLSearchParams(window.location.search);
  return params.get('startapp') || params.get('start_param') || '';
}

export function triggerHapticImpact(style: 'soft' | 'light' | 'medium' = 'light'): void {
  try {
    if (
      window.WebApp?.HapticFeedback &&
      (window.WebApp.platform === 'ios' || window.WebApp.platform === 'android')
    ) {
      window.WebApp.HapticFeedback.impactOccurred(style);
    }
  } catch {
    // Ignore on unsupported platforms
  }
}

export function triggerHapticSuccess(): void {
  try {
    if (
      window.WebApp?.HapticFeedback &&
      (window.WebApp.platform === 'ios' || window.WebApp.platform === 'android')
    ) {
      window.WebApp.HapticFeedback.notificationOccurred('success');
    }
  } catch {
    // Ignore on unsupported platforms
  }
}

