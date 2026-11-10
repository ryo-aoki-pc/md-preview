import playableLink from './playable-link.mjs';
export default function videoLink(tree, state) {
  return playableLink(tree, { ...state, mediaType: 'video' });
}
