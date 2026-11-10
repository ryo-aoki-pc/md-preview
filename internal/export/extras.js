(function renderExtras() {
  const content = document.getElementById('gp-content');
  const root = document.documentElement;
  const failure = (out, source, label) => {
    out.className = 'gp-error';
    out.textContent = source + '\n' + label;
  };
  const math = () => {
    for (const el of content.querySelectorAll('.js-render-math')) {
      const display = el.getAttribute('data-math-style') === 'display';
      const box = display ? el.closest('.markdown-code-block') || el.closest('pre') || el : el;
      const out = document.createElement(display ? 'div' : 'span');
      out.className = display ? 'gp-math-display' : 'gp-math-inline';
      try {
        window.katex.render(el.textContent, out, {
          displayMode: display,
          throwOnError: false,
          maxSize: 20,
          maxExpand: 1000,
          trust: false,
        });
      } catch {
        failure(out, el.textContent, '数式を描画できません');
      }
      box.replaceWith(out);
    }
  };
  const codes = [...content.querySelectorAll('.js-render-mermaid')].map((el) => ({
    source: el.textContent,
    box: el.closest('.markdown-code-block') || el.closest('pre') || el,
  }));
  let sequence = 0,
    generation = 0;
  const diagrams = async () => {
    if (!codes.length) return;
    const pass = ++generation;
    const dark =
      root.dataset.theme === 'dark' ||
      (root.dataset.theme === 'system' && matchMedia('(prefers-color-scheme:dark)').matches);
    window.mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: dark ? 'dark' : 'default' });
    for (const item of codes) {
      const out = document.createElement('div');
      out.className = 'gp-mermaid';
      const id = 'gp-mermaid-' + ++sequence;
      try {
        const { svg } = await window.mermaid.render(id, item.source);
        if (pass !== generation) return;
        out.innerHTML = svg;
      } catch {
        document.getElementById('d' + id)?.remove();
        if (pass !== generation) return;
        failure(out, item.source, 'mermaid の図を描画できません');
      }
      item.box.replaceWith(out);
      item.box = out;
    }
  };
  math();
  void diagrams();
  if (root.dataset.theme === 'system')
    matchMedia('(prefers-color-scheme:dark)').addEventListener('change', () => void diagrams());
})();