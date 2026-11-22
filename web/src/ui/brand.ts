/** FormNET's app mark, the same artwork as the desktop icon, the splash and the favicon (assets/formnet_logo.svg: a
 *  node with three arcs of traffic leaving it), drawn inline on the family's rounded tile so it paints with the first
 *  frame. Every id is namespaced per copy, so two marks on screen never share a gradient. The splash page
 *  (src-tauri/splash/index.html) carries the same markup with the prefix "splash-". */
export const MARK = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><defs><linearGradient id="{p}tile" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="32"><stop offset="0" stop-color="#1C1C22" /><stop offset="1" stop-color="#0A0A0D" /></linearGradient><linearGradient id="{p}rim" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="0" y2="32"><stop offset="0" stop-color="#FFFFFF" stop-opacity="0.16" /><stop offset="0.35" stop-color="#FFFFFF" stop-opacity="0.03" /><stop offset="1" stop-color="#FFFFFF" stop-opacity="0.07" /></linearGradient><linearGradient id="{p}paint" gradientUnits="userSpaceOnUse" x1="6" y1="0" x2="28" y2="0"><stop offset="0" stop-color="#FFAE2B" /><stop offset="0.5" stop-color="#F97316" /><stop offset="1" stop-color="#FF4F7A" /></linearGradient><radialGradient id="{p}bloom" gradientUnits="userSpaceOnUse" cx="10" cy="16" r="14"><stop offset="0" stop-color="#F97316" stop-opacity="0.22" /><stop offset="1" stop-color="#F97316" stop-opacity="0" /></radialGradient></defs><rect width="32" height="32" rx="7" ry="7" fill="url(#{p}tile)" /><rect width="32" height="32" rx="7" ry="7" fill="url(#{p}bloom)" /><circle cx="9.5" cy="16" r="3" fill="url(#{p}paint)" /><g fill="none" stroke="url(#{p}paint)" stroke-width="2.1" stroke-linecap="round"><path d="M 14.5 11 A 7 7 0 0 1 14.5 21" /><path d="M 18.5 7.5 A 12 12 0 0 1 18.5 24.5" /><path d="M 22.5 4 A 17 17 0 0 1 22.5 28" /></g><rect x="0.2" y="0.2" width="31.6" height="31.6" rx="6.8" ry="6.8" fill="none" stroke="url(#{p}rim)" stroke-width=".4px" /></svg>`;

let copies = 0;

export function brandMark(size: number, label = 'FormNET'): string {
  const p = `cv-mark${++copies}-`;
  return MARK.replace(/\{p\}/g, p).replace('<svg ', `<svg class="cv-mark" width="${size}" height="${size}" role="img" aria-label="${label}" `);
}
