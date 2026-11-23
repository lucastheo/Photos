import { LitElement, css, html } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { ApiClient, ApiError } from '../api/client';
import { sharedStyles } from '../styles/shared';
import type { SessionResponse } from '../types/api';

@customElement('photo-login-view')
export class PhotoLoginView extends LitElement {
  static override styles = [
    sharedStyles,
    css`
      :host {
        display: grid;
        place-items: center;
        min-block-size: 60dvh;
      }

      .login-card {
        inline-size: min(100%, 26rem);
        display: grid;
        gap: 1rem;
      }

      h2 {
        margin: 0;
        font-size: 1.25rem;
      }

      form {
        display: grid;
        gap: 0.85rem;
      }

      .field {
        display: grid;
        gap: 0.35rem;
      }

      .password-row {
        display: flex;
        gap: 0.5rem;
      }

      input[type='password'],
      input[type='text'] {
        flex: 1 1 auto;
        min-inline-size: 0;
        padding: 0.5rem 0.7rem;
        border-radius: 0.5rem;
        border: 1px solid var(--border-color, #cbd5e1);
        background: var(--bg-canvas, #f8fafc);
      }
    `,
  ];

  @property({ attribute: false })
  api = new ApiClient();

  @state()
  private password = '';

  @state()
  private showPassword = false;

  @state()
  private submitting = false;

  @state()
  private errorMessage = '';

  private handlePasswordInput(e: Event): void {
    const input = e.target as HTMLInputElement;
    this.password = input.value;
  }

  private toggleShowPassword(): void {
    this.showPassword = !this.showPassword;
  }

  private async handleSubmit(e: Event): Promise<void> {
    e.preventDefault();
    if (this.submitting) return;
    this.submitting = true;
    this.errorMessage = '';
    try {
      const session = await this.api.login(this.password);
      this.password = '';
      this.dispatchEvent(
        new CustomEvent<SessionResponse>('login-success', {
          detail: session,
          bubbles: true,
          composed: true,
        }),
      );
    } catch (err) {
      this.errorMessage =
        err instanceof ApiError ? err.message : 'Unable to sign in. Please check your password.';
    } finally {
      this.submitting = false;
    }
  }

  override render() {
    return html`
      <section class="card login-card" aria-labelledby="login-heading">
        <h2 id="login-heading">Sign in to Photo View Tracker</h2>
        <p class="muted">Enter your library password to view and track your photos.</p>
        ${this.errorMessage
          ? html`<p class="error-banner" role="alert">${this.errorMessage}</p>`
          : null}
        <form @submit=${this.handleSubmit}>
          <div class="field">
            <label for="current-password">Password</label>
            <div class="password-row">
              <input
                id="current-password"
                name="password"
                .type=${this.showPassword ? 'text' : 'password'}
                autocomplete="current-password"
                enterkeyhint="done"
                required
                .value=${this.password}
                @input=${this.handlePasswordInput}
              />
              <button
                type="button"
                class="btn"
                aria-pressed=${this.showPassword ? 'true' : 'false'}
                @click=${this.toggleShowPassword}
              >
                ${this.showPassword ? 'Hide password' : 'Show password'}
              </button>
            </div>
          </div>
          <button type="submit" class="btn btn-primary" ?disabled=${this.submitting}>
            ${this.submitting ? 'Signing In…' : 'Sign In'}
          </button>
        </form>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'photo-login-view': PhotoLoginView;
  }
}
