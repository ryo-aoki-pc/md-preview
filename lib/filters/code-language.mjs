import { elements, element, onlyCode, classes } from '../tree.mjs';
export default function codeLanguage(tree, state) {
  for (const { node: pre } of elements(tree, (n) => n.tagName === 'pre')) {
    if (pre.properties.lang !== undefined && !elements(pre, (n) => n.tagName === 'code').length)
      pre.children = [element('code', {}, pre.children)];
    const code = onlyCode(pre);
    if (!code) continue;
    const p = pre.properties,
      c = code.properties;
    const language =
      p.dataLang ??
      c.dataLang ??
      classes(code)
        .find((s) => s.startsWith('language-'))
        ?.slice(9) ??
      p.lang ??
      c.lang;
    const meta = p.dataMeta ?? c.dataMeta;
    const colon = String(language ?? '').indexOf(':');
    const lang = colon < 0 ? language : language.slice(0, colon);
    const params =
      (colon < 0 ? '' : language.slice(colon + 1).trim()) || (String(meta ?? '').trim() ? meta : undefined);
    delete c.lang;
    delete c.dataLang;
    delete p.lang;
    delete p.dataMeta;
    delete c.dataMeta;
    if (classes(code).some((s) => s.startsWith('language-'))) {
      const remaining = classes(code).filter((s) => !s.startsWith('language-'));
      if (remaining.length) c.className = remaining;
      else delete c.className;
    }
    if (lang && String(lang).trim()) {
      p.dataLang = lang;
      p.dataCanonicalLang = lang;
      if (params !== undefined && params !== null && params !== '') p.dataLangParams = params;
    }
  }
  return tree;
}
