export const root = document.querySelector("#app");

const toast = document.querySelector("#toast");
const footer = document.querySelector("#page-footer");
let toastTimer;

// instantiateTemplate clones a native HTML template and returns its named references.
export function instantiateTemplate(id) {
  const template = document.querySelector(`#${id}`);
  if (!(template instanceof HTMLTemplateElement)) {
    throw new Error(`missing HTML template: ${id}`);
  }

  const fragment = template.content.cloneNode(true);
  const refs = {};
  fragment.querySelectorAll("[data-ref]").forEach((node) => {
    refs[node.dataset.ref] = node;
  });
  return { fragment, refs };
}

// templateElement clones a template that has exactly one top-level element.
export function templateElement(id) {
  const { fragment, refs } = instantiateTemplate(id);
  const element = fragment.firstElementChild;
  if (!(element instanceof Element) || element.nextElementSibling) {
    throw new Error(`template must contain exactly one root element: ${id}`);
  }
  return { element, refs };
}

// topbar creates the shared ScreenDeck navigation header from static HTML.
export function topbar(action) {
  const { element: bar } = templateElement("topbar-template");
  if (action) bar.append(action);
  return bar;
}

// updateFooter refreshes the fixed footer content with runtime version information.
export function updateFooter(config = {}) {
  if (!footer) return;
  const year = new Date().getFullYear();
  const version = String(config.version || "dev").trim() || "dev";
  footer.textContent = `© ${year} ScreenDeck · Version ${version}`;
}

// backButton creates a button that returns to the previous application view.
export function backButton(onBack) {
  const { element: button } = templateElement("back-button-template");
  button.onclick = onBack;
  return button;
}

// messageElement creates one styled message from static markup.
export function messageElement(templateID, message) {
  const { element, refs } = templateElement(templateID);
  refs.message.textContent = message;
  return element;
}

// loadingElement creates a branded loading state with an accessible message.
export function loadingElement(message) {
  const loading = document.createElement("div");
  loading.className = "loading";
  loading.setAttribute("role", "status");

  const mark = document.createElement("img");
  mark.className = "loading-mark";
  mark.src = "/favicon.svg";
  mark.alt = "";
  mark.setAttribute("aria-hidden", "true");

  const label = document.createElement("p");
  label.textContent = message;
  loading.append(mark, label);
  return loading;
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
    const { element: dialog, refs } = templateElement(
      "confirm-dialog-template",
    );
    dialog.setAttribute("aria-labelledby", "confirm-dialog-title");
    dialog.setAttribute("aria-describedby", "confirm-dialog-message");

    refs.title.textContent = title;
    refs.message.textContent = message;
    refs.cancel.textContent = cancelLabel;
    refs.confirm.textContent = confirmLabel;
    refs.confirm.className = destructive ? "btn danger" : "btn primary";

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
    refs.cancel.focus();
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

// clearFieldErrors removes field-level validation feedback from a form.
export function clearFieldErrors(form) {
  form
    .querySelectorAll("[data-validation-error]")
    .forEach((node) => node.remove());
  form.querySelectorAll('[aria-invalid="true"]').forEach((node) => {
    node.removeAttribute("aria-invalid");
    node.removeAttribute("aria-errormessage");
  });
}

// showFieldErrors renders API validation problems beside their form controls.
export function showFieldErrors(form, error, fields) {
  clearFieldErrors(form);
  let rendered = false;
  Object.entries(error.problems || {}).forEach(([field, message]) => {
    const control = fields[field];
    if (!(control instanceof Element)) return;

    control.setAttribute("aria-invalid", "true");
    const feedback = document.createElement("p");
    feedback.className = "field-error";
    feedback.dataset.validationError = field;
    feedback.id = `validation-${field.replace(/[^a-z0-9]+/gi, "-")}`;
    feedback.textContent = message;
    control.setAttribute("aria-errormessage", feedback.id);
    control.insertAdjacentElement("afterend", feedback);

    const clear = () => {
      control.removeAttribute("aria-invalid");
      control.removeAttribute("aria-errormessage");
      feedback.remove();
    };
    control.addEventListener("input", clear, { once: true });
    control.addEventListener("change", clear, { once: true });
    rendered = true;
  });
  return rendered;
}

// showModalDialog opens a dialog and removes it after backdrop or explicit closure.
export function showModalDialog(dialog, onClose) {
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
  dialog.addEventListener(
    "close",
    () => {
      dialog.remove();
      onClose?.();
    },
    { once: true },
  );
  document.body.append(dialog);
  dialog.showModal();
}
