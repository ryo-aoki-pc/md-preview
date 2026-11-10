import playableLink from './playable-link.mjs';
export default function audioLink(tree, state) {
  return playableLink(tree, { ...state, mediaType: 'audio' });
}
