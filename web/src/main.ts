import { h } from './dom.ts';
import { boot as bootSession } from './session.ts';
import { Store } from './state.ts';
import { startApp } from './ui/app.ts';
import { revealApp } from './ui/dom';
import { startLanguage } from './ui/i18n';
import { applyTheme } from './ui/themes';

/** No session: the page was opened without this run's token. The views never mount; the note says where the address is. */
function lostSession(): void {
  const root = document.getElementById('app');
  root?.replaceChildren(
    h('div', { class: 'session-lost', role: 'alert' },
      h('h1', {}, 'No session'),
      h('p', {}, 'This formNET page has no session. Open formNET from the address printed by ', h('code', {}, 'formnet serve'), ', or run ', h('code', {}, 'formnet open'), '.')),
  );
  revealApp();
}

// The theme first (the boot script painted the surfaces; this re-applies the same values and owns later changes),
// then the language table, then the window, built once.
applyTheme();
void startLanguage().then(() => {
  if (!bootSession()) { lostSession(); return; }
  startApp(new Store(), lostSession);
});
