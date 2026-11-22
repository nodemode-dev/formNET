import type { DockAdapter, DockAgentStatus, DockChannel, DockErrorKind, DockTerminal, PtyKind } from '../dock/adapter';
import { authHeaders, token } from '../session.ts';
import { isMac } from './dom';
import { t } from './i18n';
import { terminalCapture } from './prefs';

/** FormNET's side of the family's terminal dock (src/dock, module 2.2.1, copied unchanged from formvis): the adapter
 *  turns the module's calls into FormNET's /api/terminal routes and socket. Every route carries the run's token as a
 *  bearer header and the socket as ?token=, as the event stream does. */
export type OwnKind = 'antigravity';
type Kind = Exclude<PtyKind, 'agent'> | OwnKind;
const KINDS: readonly Kind[] = ['shell', 'claude', 'codex', 'cursor', 'antigravity'];
const PROGRAMS = ['claude', 'codex', 'cursor', 'antigravity'] as const;
const WORDS = { app: 'FormNET', noun: 'recording' } as const;

export const DOCK_KEY = 'formnet.agentDock';
export const DOCK_SIZES = { minHeight: 120, maxHeightShare: 0.7, defaultHeight: 280, minWidth: 320, maxWidthShare: 0.6, defaultWidth: 440 };

/** An answer from the engine that was not a success: the HTTP status and the engine's fixed code ('' when it sent none). */
export class DockError extends Error {
  constructor(message: string, readonly status: number, readonly code: string) { super(message); }
}

async function call<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const headers: Record<string, string> = { ...authHeaders(), ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) };
  const r = await fetch(path, { method, credentials: 'omit', headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  const said = (await r.json().catch(() => ({}))) as { error?: unknown; code?: unknown };
  if (!r.ok) throw new DockError(typeof said.error === 'string' ? said.error : r.statusText, r.status, typeof said.code === 'string' ? said.code : '');
  return said as T;
}

interface EngineTerminal { id: string; title: string; kind?: string; alive?: boolean; tip?: string; status?: string }
interface EngineList { terminals?: EngineTerminal[] | null; kinds?: string[] | null; available?: boolean }
interface EngineProgram { installed?: unknown; version?: unknown; install?: unknown; signedIn?: unknown; finding?: unknown; said?: unknown }

const text = (v: unknown): string => (typeof v === 'string' ? v : '');
const record = (x: EngineTerminal): DockTerminal<OwnKind> => ({
  id: x.id, title: x.title, alive: x.alive !== false, kind: KINDS.find((k) => k === x.kind) ?? 'shell',
  ...(x.tip ? { tip: x.tip } : {}), ...(x.status ? { status: x.status } : {}),
});

/** The socket: output arrives as binary frames (the first is always the scrollback replay, possibly empty, and is
 *  passed on even when empty, so the module knows the engine answered); the exit is a text frame. */
function socketChannel(id: string): DockChannel {
  const socket = new WebSocket(`ws://${location.host}/api/terminal/${encodeURIComponent(id)}/stream?token=${encodeURIComponent(token())}`);
  let data = (_chunk: string | Uint8Array): void => undefined;
  let exit = (): void => undefined;
  let closed = (): void => undefined;
  socket.binaryType = 'arraybuffer';
  socket.addEventListener('message', (msg) => {
    if (msg.data instanceof ArrayBuffer) { data(new Uint8Array(msg.data)); return; }
    try { if ((JSON.parse(String(msg.data)) as { type?: string }).type === 'exit') exit(); } catch { return; }
  });
  socket.addEventListener('close', () => closed());
  const send = (frame: string): void => {
    if (socket.readyState === WebSocket.OPEN) socket.send(frame);
    else if (socket.readyState === WebSocket.CONNECTING) socket.addEventListener('open', () => socket.send(frame), { once: true });
  };
  return {
    send: (chunk) => send(JSON.stringify({ type: 'input', data: chunk })),
    resize: (cols, rows) => send(JSON.stringify({ type: 'resize', cols, rows })),
    onData: (handler) => { data = handler; },
    onExit: (handler) => { exit = handler; },
    onClose: (handler) => { closed = handler; },
    close: () => socket.close(),
  };
}

export interface DockHost {
  openSettings(): void;
  layerOpen(): boolean;
  /** The engine said whether this computer can run terminals; the dock is refreshed when that changes what it offers. */
  changed(): void;
}

export function formnetDock(host: DockHost): DockAdapter<OwnKind> {
  let offered: Kind[] = [...KINDS];
  const learn = (list: EngineList): void => {
    const kinds = Array.isArray(list.kinds) ? list.kinds : KINDS;
    const next = list.available === false ? [] : KINDS.filter((k) => kinds.includes(k));
    if (next.join() === offered.join()) return;
    offered = next;
    host.changed();
  };
  const mod = (e: KeyboardEvent): boolean => (isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey);
  const inDock = (e: KeyboardEvent): boolean => e.target instanceof Element && e.target.closest('.ft-dock') !== null;
  return {
    words: { ...WORDS, example: 'which hosts did this app call?', lead: 'Terminals for this computer, with Claude Code and the other agent programs it has.' },
    strings: {
      empty: 'No terminal is open. Start a shell with the plus button, or Claude Code with the agent button.',
      askPlaceholder: 'Ask Claude Code',
      tourPanelBody: 'This panel holds terminals: a shell, Claude Code and the other agent programs on this computer. It stays out of the way until you open it.',
      tourChatBody: 'Type what you want in this line, for example "{example}", and press Enter. Claude Code opens in a tab and starts on it.',
    },
    placement: { mode: 'own', storage: { key: DOCK_KEY }, sizes: DOCK_SIZES },
    isDockChord: (e) => mod(e) && !e.shiftKey && !e.altKey && e.code === 'KeyJ' && !(host.layerOpen() && !inDock(e)),
    chordLabel: () => `${isMac ? 'Cmd' : 'Ctrl'}+J`,
    passKey: (e) => e.key === 'F1' || (mod(e) && !e.altKey && e.key === ','),
    tabs: () => ({ builtin: offered }),
    classes: { iconButton: 'cv-tt-btn', button: 'cv-btn', primaryButton: 'cv-btn-primary', input: 'cv-input', select: 'cv-input', note: 'cv-note' },
    t: (_key, english) => t(english.replace(/\{(app|noun)\}/g, (_m, name: 'app' | 'noun') => WORDS[name])),
    onTheme: (handler) => { document.addEventListener('nm:ui-settings-changed', handler); },
    keyHeaders: authHeaders,
    firstRun: () => false,
    errors: {
      message: (err) => (err instanceof DockError ? err.message : null),
      kind: (err): DockErrorKind | null => {
        if (err instanceof TypeError) return 'unreachable';
        if (!(err instanceof DockError)) return null;
        if (err.code === 'not_installed') return 'notInstalled';
        if (err.code === 'busy') return 'busy';
        if (err.code === 'forbidden' || err.code === 'unavailable') return 'refused';
        if (err.status === 404 && !err.code && err.message === 'no such route') return 'oldEngine';
        return null;
      },
    },
    terminal: {
      list: async () => {
        const list = await call<EngineList>('/api/terminal');
        learn(list);
        return (list.terminals ?? []).map(record);
      },
      open: async (kind, o) => record(await call<EngineTerminal>('/api/terminal', 'POST', {
        kind, cols: o.cols, rows: o.rows, ...(o.prompt ? { prompt: o.prompt } : {}), ...(o.resume ? { resume: o.resume } : {}), ...(terminalCapture() ? { capture: true } : {}),
      })),
      restart: async (id) => { await call(`/api/terminal/${encodeURIComponent(id)}/restart`, 'POST', {}); },
      close: async (id) => { await call(`/api/terminal/${encodeURIComponent(id)}`, 'DELETE'); },
      channel: (id) => socketChannel(id),
    },
    agentStatus: async () => {
      let said: Partial<Record<(typeof PROGRAMS)[number], EngineProgram>>;
      try { said = await call('/api/terminal/agents'); } catch { return null; }
      const out: DockAgentStatus<OwnKind> = {};
      for (const kind of PROGRAMS) {
        const s = said[kind];
        if (!s) continue;
        out[kind] = {
          installed: s.installed === true, version: text(s.version) || null, install: text(s.install), signedIn: typeof s.signedIn === 'boolean' ? s.signedIn : null,
          ...(s.finding !== 'runs' && text(s.said) ? { note: text(s.said) } : {}),
        };
      }
      return out;
    },
    openSettings: () => host.openSettings(),
  };
}
