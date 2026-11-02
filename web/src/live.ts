import { api, onUnauthorized, openStream, SessionError } from './api.ts';
import type { Store } from './state.ts';
import { applyFlowEvents, withFlows } from './state.ts';
import type { FlowEvent } from './types.ts';

/** Connects the store to the server: the event stream, the status, the hosts and the first machine sample. Stream
 *  events are collected and applied once per animation frame. Returns the way to stop. */
export function connectLive(store: Store, onLost: () => void): () => void {
  const sayError = (what: string) => (err: Error) => {
    if (err instanceof SessionError) return;
    store.update({ notice: `${what}: ${err.message}` });
  };
  const refreshStatus = (): void => { api.status().then((status) => store.update({ status })).catch(sayError('status')); };
  const refreshHosts = (): void => { api.hosts().then((r) => store.update({ hosts: r.hosts ?? [] })).catch(sayError('hosts')); };
  const reloadFlows = (): void => {
    api.flows({ limit: store.get().limit }).then((r) => store.update((s) => withFlows(s, r.flows ?? [], r.total))).catch(sayError('flows'));
  };
  let hostsTimer = 0;
  const scheduleHosts = (): void => {
    if (hostsTimer) return;
    hostsTimer = window.setTimeout(() => { hostsTimer = 0; refreshHosts(); }, 500);
  };
  let pending: FlowEvent[] = [];
  let frame = 0;
  const flush = (): void => {
    frame = 0;
    const batch = pending;
    pending = [];
    if (batch.length === 0) return;
    store.update((s) => applyFlowEvents(s, batch));
    scheduleHosts();
  };
  let lost = false;
  const lostSession = (): void => {
    if (lost) return;
    lost = true;
    stop();
    store.update({ sessionLost: true, connected: false });
    onLost();
  };
  onUnauthorized(lostSession);
  const stopStream = openStream({
    hello: (hello) => {
      const s = store.get();
      const epoch = hello.epoch ?? '';
      store.update({ connected: true, epoch });
      reloadFlows();
      if (s.epoch && epoch === s.epoch && hello.dropped) store.update({ notice: `${hello.dropped} events were missed; reloaded` });
      refreshStatus();
      refreshHosts();
      api.machine().then((machine) => store.update({ machine })).catch(() => undefined);
    },
    resync: () => { pending = []; reloadFlows(); },
    close: () => store.update({ connected: false }),
    unauthorized: lostSession,
    flow: (e) => { pending = [...pending, e]; if (!frame) frame = requestAnimationFrame(flush); },
    machine: (machine) => store.update({ machine }),
  });
  const timer = window.setInterval(() => { if (document.visibilityState === 'visible' && !store.get().sessionLost) refreshStatus(); }, 10000);
  const stop = (): void => { stopStream(); window.clearInterval(timer); };
  return stop;
}
