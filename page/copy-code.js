(() => {
  'use strict';
  if (customElements.get('copy-code')) return;

  // 一時的な入力欄は本文の外へ置き、差分更新で照合する子の並びを変えない。
  function copyFallback(text) {
    let focused = document.activeElement;
    while (focused?.shadowRoot?.activeElement) focused = focused.shadowRoot.activeElement;
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.setAttribute('aria-label', 'コピー用の一時的な欄');
    Object.assign(textarea.style, { position: 'fixed', left: '0', top: '0', opacity: '0', pointerEvents: 'none' });
    try {
      document.body.appendChild(textarea);
      textarea.focus({ preventScroll: true });
      textarea.select();
      return document.execCommand('copy');
    } finally {
      textarea.remove();
      if (focused?.isConnected && focused.focus) focused.focus({ preventScroll: true });
    }
  }

  class CopyCode extends HTMLElement {
    constructor() {
      super();
      const root = this.attachShadow({ mode: 'open' });
      const style = document.createElement('style');
      style.textContent = `
        button {
          display: block;
          padding: 4px 8px;
          color: var(--gp-fg);
          background: var(--gp-subtle);
          border: 1px solid var(--gp-border);
          border-radius: 4px;
          font: 12px/1.5 sans-serif;
          cursor: pointer;
        }
        button:focus-visible { outline: 2px solid var(--gp-link, #1068bf); outline-offset: 2px; }
      `;
      this.button = document.createElement('button');
      this.button.type = 'button';
      this.button.textContent = 'コピー';
      this.button.setAttribute('aria-live', 'polite');
      root.append(style, this.button);
      this.button.addEventListener('click', () => void this.copy());
    }

    disconnectedCallback() {
      clearTimeout(this.timer);
      this.button.textContent = 'コピー';
    }

    async copy() {
      const wrapper = this.parentElement;
      if (!wrapper?.matches('.markdown-code-block')) return;
      const code = wrapper.querySelector(':scope > pre > code');
      if (!code) return;
      const text = code.textContent;
      let copied = false;
      try {
        // 利用者のクリックから、ほかの非同期処理を挟まずに呼ぶ。
        if (navigator.clipboard?.writeText) {
          await navigator.clipboard.writeText(text);
          copied = true;
        }
      } catch {
        // 許可されない場合も、一時的な欄で文字だけのコピーを試す。
      }
      if (!copied) {
        try { copied = copyFallback(text); }
        catch { /* コピーを使えない場合はボタンで知らせる。 */ }
      }
      if (!this.isConnected) return;
      clearTimeout(this.timer);
      this.button.textContent = copied ? 'コピーしました' : 'コピーできませんでした';
      if (copied) this.timer = setTimeout(() => { this.button.textContent = 'コピー'; }, 2000);
    }
  }

  customElements.define('copy-code', CopyCode);
})();
