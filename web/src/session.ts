// The page's session is a bearer token the server printed and put in the
// opening address (/?token=<hex>). There is no cookie: a cookie for 127.0.0.1
// would travel to every other local server the person visits. The token is
// read once from the address, kept here and in sessionStorage so a reload
// survives, and removed from the address bar.

const KEY = "formnet.token";
let current = "";

function readStored(): string {
  try {
    return sessionStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

function store(value: string): void {
  try {
    sessionStorage.setItem(KEY, value);
  } catch {
    // storage unavailable; the token lives in this module for this page
  }
}

// boot reads the token from the address or from sessionStorage and returns
// whether the page has one.
export function boot(): boolean {
  const params = new URLSearchParams(location.search);
  const fromURL = params.get("token") ?? "";
  if (fromURL) {
    current = fromURL;
    store(fromURL);
    history.replaceState(null, "", "/");
  } else {
    current = readStored();
  }
  return current !== "";
}

export function token(): string {
  return current;
}

// withToken appends the token as a query parameter, for navigations and the
// event stream, where a header cannot be set.
export function withToken(url: string): string {
  if (!current) return url;
  return `${url}${url.includes("?") ? "&" : "?"}token=${encodeURIComponent(current)}`;
}

// authHeaders are sent with every fetch.
export function authHeaders(): Record<string, string> {
  return current ? { Authorization: `Bearer ${current}` } : {};
}
