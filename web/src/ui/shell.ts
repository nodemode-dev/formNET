import { api } from "../api.ts";
import { exportFilter } from "../desktop.ts";
import { h } from "../dom.ts";
import type { State, Store, View } from "../state.ts";
import { visibleFlows } from "../state.ts";

const views: { id: View; label: string }[] = [
  { id: "flows", label: "Flows" },
  { id: "hosts", label: "Hosts" },
  { id: "machine", label: "Machine" },
  { id: "setup", label: "Setup" },
];

// Shell is the top bar: views, filter, live toggle, clear, export and counts.
export function shell(store: Store): HTMLElement {
  const tabs = views.map((v) =>
    h(
      "button",
      { type: "button", class: "tab", "data-view": v.id, role: "tab", onClick: () => store.update({ view: v.id }) },
      v.label,
    ),
  );
  const filter = h("input", {
    type: "search",
    class: "filter",
    placeholder: "Filter flows (press / )",
    "aria-label": "Filter flows",
    autocomplete: "off",
    spellcheck: "false",
  }) as HTMLInputElement;
  filter.addEventListener("input", () => store.update({ filter: filter.value }));
  const hostChip = h("button", { type: "button", class: "chip", title: "Remove the host filter", onClick: () => store.update({ hostFilter: "" }) });
  const live = h("button", {
    type: "button",
    class: "btn",
    "aria-pressed": "true",
    onClick: () => store.update((s) => ({ live: !s.live })),
  });
  const clearBtn = h("button", {
    type: "button",
    class: "btn",
    onClick: () => {
      api.clearFlows().catch((err: Error) => store.update({ notice: err.message }));
    },
  }, "Clear");
  const exportLink = h("a", { class: "btn", download: "", title: "HAR of the flows matching the text and host filter, as the server applies them" }, "Export HAR") as HTMLAnchorElement;
  const counts = h("span", { class: "counts", "aria-live": "polite" });
  const conn = h("span", { class: "conn", title: "Connection to formnet" });
  const notice = h("div", { class: "notice", role: "status" });

  const bar = h(
    "header",
    { class: "topbar" },
    h("div", { class: "brand" }, h("img", { src: "/favicon.svg", alt: "", width: "20", height: "20" }), "formnet"),
    h("nav", { class: "tabs", role: "tablist" }, ...tabs),
    h("div", { class: "filterwrap" }, filter, hostChip),
    h("div", { class: "actions" }, live, clearBtn, exportLink, counts, conn),
  );
  const wrap = h("div", {}, bar, notice);

  document.addEventListener("keydown", (ev) => {
    if (ev.key === "/" && document.activeElement !== filter && !(document.activeElement instanceof HTMLInputElement)) {
      ev.preventDefault();
      filter.focus();
      filter.select();
    }
  });

  const render = (s: State) => {
    for (const t of tabs) {
      const on = t.dataset["view"] === s.view;
      t.classList.toggle("on", on);
      t.setAttribute("aria-selected", on ? "true" : "false");
    }
    if (filter.value !== s.filter) filter.value = s.filter;
    hostChip.textContent = s.hostFilter ? `host: ${s.hostFilter}  x` : "";
    hostChip.hidden = !s.hostFilter;
    live.textContent = s.live ? "Live" : "Paused";
    live.classList.toggle("on", s.live);
    live.setAttribute("aria-pressed", s.live ? "true" : "false");
    exportLink.href = api.harURL(exportFilter(s));
    const shown = visibleFlows(s).length;
    counts.textContent = shown === s.flows.size ? `${s.flows.size} flows` : `${shown} of ${s.flows.size} flows`;
    conn.textContent = s.connected ? "connected" : "reconnecting";
    conn.classList.toggle("off", !s.connected);
    notice.textContent = s.notice;
    notice.hidden = !s.notice;
  };
  store.subscribe(render);
  render(store.get());
  return wrap;
}
