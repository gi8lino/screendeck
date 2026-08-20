export const root = document.querySelector("#app");

const toast = document.querySelector("#toast");
const footer = document.querySelector("#page-footer");
let toastTimer;

// el creates a DOM element with optional class and text content.
export function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

// topbar creates the shared ScreenDeck navigation header.
export function topbar(action) {
  const bar = el("header", "topbar");
  const brand = el("a", "brand");
  brand.href = "/";
  brand.append(el("span", "mark", "S"), el("span", "", "ScreenDeck"));
  bar.append(brand);
  if (action) bar.append(action);
  return bar;
}

// updateFooter refreshes the fixed footer content with runtime version information.
export function updateFooter(config = {}) {
  if (!footer) return;
  const year = new Date().getFullYear();
  const rawVersion = String(config.version || "dev").trim() || "dev";
  const version =
    rawVersion === "dev" || rawVersion.startsWith("v")
      ? rawVersion
      : `v${rawVersion}`;
  footer.textContent = `© ${year} ScreenDeck · Version ${version}`;
}

// backButton creates a button that returns to the home screen.
export function backButton(onBack) {
  const button = el("button", "btn ghost", "Back");
  button.onclick = onBack;
  return button;
}

// showToast displays a temporary status message.
export function showToast(message) {
  toast.textContent = message;
  toast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove("show"), 2400);
}

// showError renders an error in a supplied element.
export function showError(node, error) {
  node.textContent = error.message || String(error);
}
