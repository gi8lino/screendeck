import { api } from "./api.js";
import { saveSession } from "./state.js";
import { backButton, el, root, showError, topbar } from "./ui.js";

// renderJoinRoom displays the form for joining an existing room.
export function renderJoinRoom(navigation) {
  root.replaceChildren();
  root.append(topbar(backButton(navigation.renderHome)));
  const panel = el("section", "panel");
  panel.append(
    el("div", "eyebrow", "Join friends"),
    el("h2", "", "Enter the room."),
  );
  const form = el("form");
  const codeRow = el("div", "form-row");
  codeRow.append(el("label", "", "Room code"));
  const code = el("input", "code-input");
  code.type = "text";
  code.maxLength = 6;
  code.required = true;
  code.placeholder = "ABC123";
  code.autocapitalize = "characters";
  codeRow.append(code);
  const nameRow = el("div", "form-row");
  nameRow.append(el("label", "", "Your name"));
  const name = el("input");
  name.type = "text";
  name.maxLength = 30;
  name.required = true;
  name.placeholder = "Deckard";
  name.autocomplete = "nickname";
  nameRow.append(name);
  const error = el("p", "error");
  const submit = el("button", "btn primary", "Join room");
  submit.type = "submit";
  form.append(codeRow, nameRow, error, submit);
  panel.append(form);
  root.append(panel);

  form.onsubmit = async (event) => {
    event.preventDefault();
    error.textContent = "";
    submit.disabled = true;
    try {
      const joined = await api("/api/rooms/join", {
        method: "POST",
        body: JSON.stringify({
          code: code.value.toUpperCase(),
          name: name.value,
        }),
      });
      saveSession(joined);
      await navigation.renderRoom();
    } catch (requestError) {
      showError(error, requestError);
      submit.disabled = false;
    }
  };
}
