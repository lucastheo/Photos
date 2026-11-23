import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, ApiError } from '../api/client';
import './login-view';
import type { PhotoLoginView } from './login-view';

describe('PhotoLoginView', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('toggles password visibility and emits login-success on valid submit', async () => {
    const api = new ApiClient();
    const loginSpy = vi
      .spyOn(api, 'login')
      .mockResolvedValue({ authenticated: true, auth_disabled: false });

    const el = document.createElement('photo-login-view') as PhotoLoginView;
    el.api = api;
    document.body.appendChild(el);
    await el.updateComplete;

    const input = el.shadowRoot?.querySelector<HTMLInputElement>('#current-password');
    const toggleBtn = el.shadowRoot?.querySelector<HTMLButtonElement>('button[type="button"]');
    const form = el.shadowRoot?.querySelector('form');

    expect(input?.getAttribute('autocomplete')).toBe('current-password');
    expect(input?.type).toBe('password');

    toggleBtn?.click();
    await el.updateComplete;
    expect(input?.type).toBe('text');
    expect(toggleBtn?.textContent?.trim()).toBe('Hide password');

    if (input) {
      input.value = 'my-secret';
      input.dispatchEvent(new Event('input'));
    }
    const successPromise = new Promise<CustomEvent>((resolve) => {
      el.addEventListener('login-success', (e) => resolve(e as CustomEvent), { once: true });
    });

    form?.dispatchEvent(new Event('submit', { cancelable: true }));
    const ev = await successPromise;
    await el.updateComplete;

    expect(loginSpy).toHaveBeenCalledWith('my-secret');
    expect(ev.detail).toEqual({ authenticated: true, auth_disabled: false });
  });

  it('renders an alert banner when login fails', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'login').mockRejectedValue(
      new ApiError(401, 'INVALID_CREDENTIALS', 'Invalid login credentials'),
    );

    const el = document.createElement('photo-login-view') as PhotoLoginView;
    el.api = api;
    document.body.appendChild(el);
    await el.updateComplete;

    const input = el.shadowRoot?.querySelector<HTMLInputElement>('#current-password');
    const form = el.shadowRoot?.querySelector('form');
    if (input) {
      input.value = 'wrong';
      input.dispatchEvent(new Event('input'));
    }
    form?.dispatchEvent(new Event('submit', { cancelable: true }));
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toBe('Invalid login credentials');
  });

  it('handles generic errors and blocks duplicate submissions', async () => {
    const api = new ApiClient();
    vi.spyOn(api, 'login').mockRejectedValue(new Error('network error'));

    const el = document.createElement('photo-login-view') as PhotoLoginView;
    el.api = api;
    document.body.appendChild(el);
    await el.updateComplete;

    const form = el.shadowRoot?.querySelector('form');
    form?.dispatchEvent(new Event('submit', { cancelable: true }));
    await new Promise((r) => setTimeout(r, 0));
    await el.updateComplete;

    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toBe('Unable to sign in. Please check your password.');
  });
});
