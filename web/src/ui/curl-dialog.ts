import { SessionError } from '../api.ts';
import { dataApi, type FlowParams } from '../api-data.ts';
import { copyText } from '../dom.ts';
import { flowFilters, TYPE_CHIPS } from '../filters.ts';
import type { Store } from '../state.ts';
import type { CurlShell } from '../types-data.ts';
import { Dialog } from './dialogs';
import { el, q, setClass, setDisabled, setText } from './dom';
import { onLanguage, t, tn } from './i18n';
import { tf } from './panels/controls';

const SHELLS: ReadonlyArray<readonly [CurlShell, string, string]> = [['sh', 'sh', 'formnet-requests.sh'], ['powershell', 'PowerShell', 'formnet-requests.ps1'], ['cmd', 'cmd', 'formnet-requests.cmd']];

const LEFT_OUT: Readonly<Record<string, string>> = { status: 'Status', method: 'Method', type: 'Type', size: 'Response size (KB)', process: 'Process', focus: 'Focus on hosts' };

const platformShell =(): CurlShell => (/Win/i.test(navigator.platform || navigator.userAgent) ? 'powershell' : 'sh');

/** File > Copy All as cURL: every http flow the filters match, as one script for the chosen shell, copied or
 *  downloaded. Credential headers are left out unless asked for, with the warning. The count says what the script will
 *  hold, from the server's own answer to the same filters. */
class CurlDialog extends Dialog {
  private readonly seg = el('<div class="cv-seg fn-curl-shells" role="radiogroup"></div>');
  private readonly secrets = el<HTMLInputElement>('<input type="checkbox">');
  private readonly warn = el('<p class="cv-note fn-curl-warn"></p>');
  private readonly count = el('<p class="fn-curl-count" role="status"></p>');
  private readonly scope = el('<p class="cv-note fn-curl-scope"></p>');
  private readonly said = el('<p class="fn-dlg-error" role="status"></p>');
  private readonly copyBtn: HTMLButtonElement;
  private readonly download = el<HTMLAnchorElement>('<a class="cv-btn cv-btn-ghost" role="button"></a>');
  private shell: CurlShell = platformShell();
  private params: FlowParams = {};
  private gen = 0;
  private http = 0;
  private timer = 0;

  constructor(private readonly store: Store) {
    super('Copy All as cURL', 500);
    q<HTMLElement>(this.scrim, '.cv-dialog').classList.add('fn-data-dlg');
    for (const [id, name] of SHELLS) {
      const b = el<HTMLButtonElement>(`<button type="button" role="radio" data-shell="${id}"></button>`);
      setText(b, name);
      b.addEventListener('click', () => { this.shell = id; this.paint(); });
      this.seg.append(b);
    }
    const shellRow = el('<div class="cv-field"><span></span></div>');
    shellRow.append(this.seg);
    this.body.append(shellRow);
    const check = el('<label class="cv-field cv-check"><span></span></label>');
    check.append(this.secrets);
    this.body.append(check, this.warn, this.count, this.scope, this.said);
    this.secrets.addEventListener('change', () => this.paint());
    this.button('Close', 'ghost', () => this.close());
    this.foot.append(this.download);
    this.copyBtn = this.button('Copy', 'primary', () => this.copy());
    this.download.addEventListener('click', (e) => { if (!this.http) e.preventDefault(); });
    const words = (): void => {
      setText(q(shellRow, 'span'), t('Shell'));
      setText(q(check, 'span'), t('Include passwords, tokens and cookies'));
      setText(this.download, t('Download'));
      this.seg.setAttribute('aria-label', t('Shell'));
      this.paint();
    };
    words();
    onLanguage(words);
  }

  ask(): void {
    const s = this.store.get();
    this.params = flowFilters.params(s.filter, s.hostFilter);
    this.secrets.checked = false;
    setText(this.said, '');
    this.http = 0;
    this.paint();
    this.recount();
    this.open(this.copyBtn);
  }

  private recount(): void {
    const gen = ++this.gen;
    setText(this.count, t('Counting the matching flows...'));
    dataApi.flows(this.params)
      .then((r) => {
        if (gen !== this.gen) return;
        const flows = r.flows ?? [];
        this.http = flows.filter((f) => f.kind === 'http').length;
        const other = flows.length - this.http;
        const lines = [tn(this.http, '{n} request matches the filters and goes into the script.', '{n} requests match the filters and go into the script.')];
        if (other) lines.push(tn(other, '{n} tunnel or WebSocket is left out.', '{n} tunnels and WebSockets are left out.'));
        setText(this.count, lines.join(' '));
        this.paint();
      })
      .catch((err: unknown) => { if (gen === this.gen && !(err instanceof SessionError)) setText(this.count, err instanceof Error ? err.message : String(err)); });
  }

  private paint(): void {
    for (const b of Array.from(this.seg.children) as HTMLElement[]) {
      const on = b.dataset.shell === this.shell;
      setClass(b, 'is-on', on);
      if (b.getAttribute('aria-checked') !== String(on)) b.setAttribute('aria-checked', String(on));
    }
    const on = this.secrets.checked;
    setText(this.warn, t(on
      ? 'The script will hold the Authorization, Proxy-Authorization, Cookie and Set-Cookie values the requests carried. Anyone who gets it can sign in as you with them. Keep it private and delete it when you are done.'
      : 'Authorization, Proxy-Authorization, Cookie and Set-Cookie values are left out, so requests that need them will be refused until you add them back.'));
    setClass(this.warn, 'is-warn', on);
    const page = flowFilters.pageOnly(this.store.get().filter);
    const chip = TYPE_CHIPS.find(([c]) => c === page[0])?.[1];
    setText(this.scope, page.length === 1 && chip ? tf('The type filter {type} is applied in this window only: the script holds every type the other filters match.', { type: t(chip) })
      : page.length ? tf('The script leaves out these filters of the options bar, so it can hold more than the list shows: {filters}.', { filters: page.map((p) => t(LEFT_OUT[p] ?? 'Type')).join(', ') })
      : t('The script uses the filters of the Flows list: the text, the host chosen in Hosts and every filter in the options bar.'));
    const file = SHELLS.find(([id]) => id === this.shell)?.[2] ?? 'formnet-requests.txt';
    const href = dataApi.curlURL(this.params, this.shell, on);
    if (this.download.getAttribute('href') !== href) this.download.href = href;
    if (this.download.getAttribute('download') !== file) this.download.setAttribute('download', file);
    setClass(this.download, 'is-off', !this.http);
    this.download.setAttribute('aria-disabled', String(!this.http));
    setDisabled(this.copyBtn, !this.http);
  }

  private copy(): void {
    if (!this.http) return;
    setDisabled(this.copyBtn, true);
    dataApi.curl(this.params, this.shell, this.secrets.checked)
      .then((text) => copyText(text))
      .then((ok) => this.say(ok ? tn(this.http, 'Copied {n} request.', 'Copied {n} requests.') : t('The clipboard was not reachable: use Download instead.')))
      .catch((err: unknown) => { if (!(err instanceof SessionError)) this.say(err instanceof Error ? err.message : String(err)); })
      .finally(() => setDisabled(this.copyBtn, !this.http));
  }

  private say(text: string): void {
    setText(this.said, text);
    window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => setText(this.said, ''), 4000);
  }
}

let dlg: CurlDialog | null = null;

/** Opens Copy All as cURL with the current filters. */
export function copyAllAsCurl(store: Store): void { (dlg ??= new CurlDialog(store)).ask(); }
