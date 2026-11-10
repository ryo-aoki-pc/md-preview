import { elements, content, element, text } from '../tree.mjs';
// Banzai::ColorParser の正規表現を、その範囲と空白条件を保ったまま移す。
const alpha = '(?:0(?:\\.\\d+)?|\\.\\d+|1(?:\\.0+)?)';
const percents = '(?:\\d{1,2}|100)%';
const channel = '(?:,[\\t\\n\\v\\f\\r ]*(?:' + alpha + '|' + percents + '))?';
const bits = '(?:\\d{1,2}|1\\d\\d|2(?:[0-4]\\d|5[0-5]))';
const degs = '-?\\d+(?:deg)?';
const rads = '-?(?:\\d+(?:\\.\\d+)?|\\.\\d+)rad';
const rgb =
  'rgba?\\((?:(?:' +
  bits +
  ',[\\t\\n\\v\\f\\r ]*){2}' +
  bits +
  '|(?:' +
  percents +
  ',[\\t\\n\\v\\f\\r ]*){2}' +
  percents +
  ')' +
  channel +
  '\\)';
const hsl =
  'hsla?\\((?:' +
  degs +
  '|' +
  rads +
  '),[\\t\\n\\v\\f\\r ]*' +
  percents +
  ',[\\t\\n\\v\\f\\r ]*' +
  percents +
  channel +
  '\\)';
const pattern = new RegExp('^(?:#[a-f\\d]{3}|#[a-f\\d]{4}|#[a-f\\d]{6}|#[a-f\\d]{8}|' + rgb + '|' + hsl + ')$', 'i');
export const parseColor = (source) => (pattern.exec(source)?.[0] === source ? source : null);
export default function color(tree, state) {
  for (const { node } of elements(tree, (n) => n.tagName === 'code')) {
    const source = content(node),
      unescaped = source.startsWith('\\') ? source.slice(1) : source;
    const parsed = parseColor(unescaped);
    if (!parsed) continue;
    if (source.startsWith('\\')) node.children = [text(unescaped)];
    else
      node.children.push(
        element('span', { className: ['gfm-color_chip'] }, [
          element('span', { style: 'background-color: ' + parsed + ';' }),
        ]),
      );
  }
  return tree;
}
