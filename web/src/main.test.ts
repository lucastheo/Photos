import { beforeEach, describe, expect, it } from 'vitest';
import './main';
import { PhotoTrackerApp } from './main';

describe('PhotoTrackerApp', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('registers the custom element and renders initial shell content', async () => {
    const el = document.createElement('photo-tracker-app');
    expect(el).toBeInstanceOf(PhotoTrackerApp);

    document.body.appendChild(el);
    await el.updateComplete;

    const heading = el.shadowRoot?.querySelector('h1');
    const summary = el.shadowRoot?.querySelector('p');
    expect(heading?.textContent).toBe('Photo View Tracker');
    expect(summary?.textContent).toBe('Personal photo & video library view tracker.');
  });
});
