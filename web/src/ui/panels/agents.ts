import { token } from '../../session';
import type { Status } from '../../types';
import { el, setText, setValue } from '../dom';
import { icon } from '../icons';
import { t } from '../i18n';
import { copyInto, note, row } from './capture';
import { setTerminalCapture, terminalCapture } from '../prefs';
import { checkRow, richText } from './controls';
import { dataFolder } from './storage';

const ROUTES = ['/api/flows', '/api/flows/{id}', '/api/hosts', '/api/machine', '/api/export/har', '/api/stream'];

/** Preferences > Agents and Scripts: how a script or an agent reads the same API as the window, with this run's token.
 *  The token is a key: masked to its last four characters, with Show and Copy, as the family shows keys. */
export class AgentsSection {
  readonly el = el('<div class="cv-panel-section cv-agents"></div>');
  private readonly address = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="API address" spellcheck="false" dir="ltr">');
  private readonly token = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Token" spellcheck="false" autocomplete="off" dir="ltr">');
  private readonly show = el<HTMLButtonElement>(`<button type="button" class="cv-btn cv-btn-ghost cv-key-show" aria-pressed="false">${icon('eye', 14)}<span></span></button>`);
  private readonly tokenPath = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Token file" spellcheck="false" dir="ltr">');
  private readonly example = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Example" spellcheck="false" dir="ltr">');
  private readonly copied = el('<p class="cv-note" role="status" style="min-height: 1.4em"></p>');
  private readonly capture = checkRow('Send the traffic of new terminal tabs to Flows', (v) => setTerminalCapture(v));
  private shown = false;

  constructor() {
    const keyBar = el('<div class="cv-improve"></div>');
    keyBar.append(copyInto('Copy token', () => token(), this.copied, 'Token copied.'), this.show);
    this.show.addEventListener('click', () => { this.shown = !this.shown; this.paintToken(); });
    const bar = el('<div class="cv-improve"></div>');
    bar.append(copyInto('Copy example', () => this.example.value, this.copied));
    const routes = el('<p class="cv-note"></p>');
    const code = (html: string): HTMLElement => el(`<code translate="no">${html}</code>`);
    const list = el('<span translate="no"></span>');
    list.innerHTML = ROUTES.map((r) => `<code>${r}</code>`).join(', ');
    richText(routes, 'Routes: {routes}. Every call carries {header} with the token; the event stream and the downloads take it as {query}.', { routes: list, header: code('Authorization: Bearer'), query: code('?token=') });
    this.el.append(row('API address', this.address), row('Token', this.token), keyBar, row('Token file', this.tokenPath), row('Example', this.example), bar, this.copied, routes,
      note('The token is this run\'s: the server writes a new one each time it starts. Loopback is not a trust boundary, since every program on this computer shares it; the token is what keeps a recording to the people who can read that file. It is shown masked; Show reveals it until this dialog closes.'),
      this.capture.el, note('A new terminal tab then gets this run\'s proxy and root certificate in its environment, so the programs started there appear in Flows. Tabs already open keep what they had.'));
    this.capture.set(terminalCapture());
    this.paintToken();
    this.refresh(null);
  }

  /** Masked: dots and the last four characters. Shown: the whole token. */
  private paintToken(): void {
    const v = token();
    setValue(this.token, this.shown ? v : v ? `${'•'.repeat(12)}${v.slice(-4)}` : '');
    setText(this.show.querySelector('span:not(.icon)'), t(this.shown ? 'Hide' : 'Show'));
    this.show.setAttribute('aria-pressed', String(this.shown));
  }

  /** Called when Preferences closes: the token goes back to masked. */
  mask(): void { if (this.shown) { this.shown = false; this.paintToken(); } }

  refresh(status: Status | null): void {
    const folder = dataFolder(status), sep = folder.includes('\\') ? '\\' : '/';
    const file = folder ? `${folder}${sep}token` : '~/formnet/token';
    this.capture.set(terminalCapture());
    setValue(this.address, location.origin);
    setValue(this.tokenPath, file);
    setValue(this.example, `curl -H "Authorization: Bearer $(cat ${file.includes(' ') ? `'${file}'` : file})" ${location.origin}/api/flows`);
  }
}
