// Small element builder so views stay readable without a framework.
import { t } from "./ui/i18n";

type Child = Node | string | number | null | undefined | false;

type Attrs = Record<string, string | number | boolean | EventListener | undefined>;

export function h(tag: string, attrs: Attrs = {}, ...children: Child[]): HTMLElement {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (value === undefined || value === false) continue;
    if (key.startsWith("on") && typeof value === "function") {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (key === "class") {
      el.className = String(value);
    } else if (value === true) {
      el.setAttribute(key, "");
    } else {
      el.setAttribute(key, String(value));
    }
  }
  append(el, children);
  return el;
}

export function append(el: Node, children: Child[]): void {
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    el.appendChild(typeof child === "object" ? child : document.createTextNode(String(child)));
  }
}

export function clear(el: Element): void {
  el.replaceChildren();
}

export function text(tag: string, cls: string, value: string | number): HTMLElement {
  return h(tag, { class: cls }, value);
}

export async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    return false;
  }
}

// copyButton copies value and shows a short confirmation on the button itself;
// label is the English, shown in the current language.
export function copyButton(label: string, value: () => string, cls = "cv-btn cv-btn-ghost"): HTMLElement {
  const btn = h("button", { type: "button", class: cls }, t(label));
  let timer = 0;
  btn.addEventListener("click", async () => {
    const ok = await copyText(value());
    btn.textContent = t(ok ? "Copied" : "Copy failed");
    window.clearTimeout(timer);
    timer = window.setTimeout(() => {
      btn.textContent = t(label);
    }, 1200);
  });
  return btn;
}
