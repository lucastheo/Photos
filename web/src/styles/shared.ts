import { css } from 'lit';

export const sharedStyles = css`
  :host {
    box-sizing: border-box;
    color: var(--text-primary, inherit);
  }

  *,
  *::before,
  *::after {
    box-sizing: border-box;
  }

  button,
  input,
  select {
    font: inherit;
    color: inherit;
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.45rem 0.8rem;
    border-radius: 0.5rem;
    border: 1px solid var(--border-color, #cbd5e1);
    background: var(--bg-surface, #ffffff);
    cursor: pointer;
    font-size: 0.875rem;
    line-height: 1.25;
  }

  .btn:hover:not(:disabled) {
    background: var(--bg-subtle, #e2e8f0);
  }

  .btn:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .btn-primary {
    background: var(--accent, #18181b);
    color: var(--accent-text, #ffffff);
    border-color: transparent;
  }

  .btn-primary:hover:not(:disabled) {
    opacity: 0.92;
    background: var(--accent, #18181b);
  }

  .btn-danger {
    color: var(--danger, #dc2626);
    border-color: var(--danger, #dc2626);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.3rem 0.65rem;
    border-radius: 999px;
    border: 1px solid var(--border-color, #cbd5e1);
    background: var(--bg-surface, #ffffff);
    font-size: 0.8125rem;
    cursor: pointer;
  }

  .chip[aria-pressed='true'],
  .chip.active {
    background: var(--accent, #18181b);
    color: var(--accent-text, #ffffff);
    border-color: transparent;
  }

  .card {
    background: var(--bg-surface, #ffffff);
    border: 1px solid var(--border-color, #cbd5e1);
    border-radius: 0.75rem;
    padding: 1rem;
  }

  .error-banner {
    padding: 0.65rem 0.85rem;
    border-radius: 0.5rem;
    border: 1px solid var(--danger, #dc2626);
    color: var(--danger, #dc2626);
    background: color-mix(in srgb, var(--danger, #dc2626) 10%, transparent);
    margin: 0.5rem 0;
  }

  .muted {
    color: var(--text-muted, #475569);
    font-size: 0.875rem;
  }
`;
