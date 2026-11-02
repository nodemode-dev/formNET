import type { Status } from '../../types';
import { el, setText, setValue } from '../dom';
import { dataFolder } from './storage';

const ROUTES = ['/api/flows', '/api/flows/{id}', '/api/hosts', '/api/machine', '/api/export/har', '/api/stream'];

/** Preferences > Agents and Scripts: how a script or an agent reads the same API as the window, with this run's token. */
export class AgentsSection {
  readonly el = el('<div class="cv-panel-section cv-agents"></div>');
  private readonly address = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="API address" spellcheck="false">');
  private readonly tokenPath = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Token file" spellcheck="false">');
  private readonly example = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Example" spellcheck="false">');
  private readonly copied = el('<p class="cv-note" role="status" style="min-height: 1.4em"></p>');

  constructor() {
    const row = (label: string, control: HTMLElement): HTMLElement => { const r = el(`<label class="cv-dialog-row"><span>${label}</span></label>`); r.appendChild(control); return r; };
    const copy = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost">Copy example</button>');
    const bar = el('<div class="cv-improve"></div>');
    bar.append(copy);
    copy.addEventListener('click', () => { void this.copy(); });
    const routes = el(`<p class="cv-note">Routes: ${ROUTES.map((r) => `<code>${r}</code>`).join(', ')}. Every call carries <code>Authorization: Bearer</code> with the token; the event stream and the downloads take it as <code>?token=</code>.</p>`);
    const note = el('<p class="cv-note">The token is this run\'s: the server writes a new one each time it starts. Loopback is not a trust boundary, since every program on this computer shares it; the token is what keeps a recording to the people who can read that file.</p>');
    this.el.append(row('API address', this.address), row('Token file', this.tokenPath), row('Example', this.example), bar, this.copied, routes, note);
    this.refresh(null);
  }

  refresh(status: Status | null): void {
    const folder = dataFolder(status), sep = folder.includes('\\') ? '\\' : '/';
    const file = folder ? `${folder}${sep}token` : '~/formnet/token';
    setValue(this.address, location.origin);
    setValue(this.tokenPath, file);
    setValue(this.example, `curl -H "Authorization: Bearer $(cat ${file.includes(' ') ? `'${file}'` : file})" ${location.origin}/api/flows`);
  }

  private async copy(): Promise<void> {
    try { await navigator.clipboard.writeText(this.example.value); setText(this.copied, 'Copied.'); }
    catch { this.example.select(); setText(this.copied, 'Select the example and copy it; the clipboard was not reachable.'); }
    window.setTimeout(() => setText(this.copied, ''), 2500);
  }
}
