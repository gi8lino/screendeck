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
  const mark = el("img", "brand-mark");
  mark.src = "/favicon.svg";
  mark.alt = "";
  mark.setAttribute("aria-hidden", "true");
  const name = el("span", "brand-name");
  name.append(
    el("span", "brand-screen", "Screen"),
    el("span", "brand-deck", "Deck"),
  );
  brand.append(mark, name);
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

// confirmAction displays a ScreenDeck-styled confirmation dialog and resolves with the user's choice.
export function confirmAction({
  title,
  message,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  destructive = false,
}) {
  return new Promise((resolve) => {
    const dialog = el("dialog", "confirm-dialog");
    dialog.setAttribute("aria-labelledby", "confirm-dialog-title");
    dialog.setAttribute("aria-describedby", "confirm-dialog-message");

    const form = el("form", "confirm-dialog-panel");
    form.method = "dialog";
    const titleNode = el("h2", "", title);
    titleNode.id = "confirm-dialog-title";
    const messageNode = el("p", "confirm-dialog-message", message);
    messageNode.id = "confirm-dialog-message";

    const actions = el("div", "confirm-dialog-actions");
    const cancel = el("button", "btn ghost", cancelLabel);
    cancel.type = "submit";
    cancel.value = "cancel";
    const confirm = el(
      "button",
      destructive ? "btn danger" : "btn primary",
      confirmLabel,
    );
    confirm.type = "submit";
    confirm.value = "confirm";
    actions.append(cancel, confirm);
    form.append(titleNode, messageNode, actions);
    dialog.append(form);

    dialog.addEventListener(
      "close",
      () => {
        const accepted = dialog.returnValue === "confirm";
        dialog.remove();
        resolve(accepted);
      },
      { once: true },
    );
    dialog.addEventListener("cancel", () => {
      dialog.returnValue = "cancel";
    });

    document.body.append(dialog);
    dialog.showModal();
    cancel.focus();
  });
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
