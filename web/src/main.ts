import { LitElement, css, html } from 'lit';
import { customElement } from 'lit/decorators.js';

@customElement('photo-tracker-app')
export class PhotoTrackerApp extends LitElement {
  static override styles = css`
    :host {
      color-scheme: light dark;
      display: block;
      font-family: system-ui, -apple-system, sans-serif;
      margin: 0 auto;
      max-width: 72rem;
      padding: 1.5rem;
    }

    h1 {
      font-size: 1.5rem;
      margin: 0 0 0.5rem;
    }

    p {
      margin: 0;
      opacity: 0.8;
    }
  `;

  override render() {
    return html`
      <main>
        <h1>Photo View Tracker</h1>
        <p>Personal photo &amp; video library view tracker.</p>
      </main>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-tracker-app': PhotoTrackerApp;
  }
}
