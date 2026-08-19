import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import { backButton, el, root, showError, topbar } from "./ui.js";

// renderJoinRoom displays the form for joining an existing room.
export function renderJoinRoom(navigation, initialCode = "") {
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
  code.value = initialCode;
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

  const genreBox = el("section", "filter-box preference-box");
  genreBox.append(
    el("div", "label", "Your genres"),
    el(
      "p",
      "muted",
      "Optional · pick what you personally want to swipe. Leave empty to see every title in the room.",
    ),
  );
  const genreStatus = el(
    "p",
    "muted",
    "Enter a valid room code to load genre choices.",
  );
  const genres = el("div", "genre-chips");
  genres.setAttribute("role", "group");
  genres.setAttribute("aria-label", "Your genres");
  genreBox.append(genreStatus, genres);

  const error = el("p", "error");
  const submit = el("button", "btn primary", "Join room");
  submit.type = "submit";
  form.append(codeRow, nameRow, genreBox, error, submit);
  panel.append(form);
  root.append(panel);
  if (initialCode) name.focus();

  let genreTimer;
  let loadedCode = "";
  const loadGenres = async () => {
    const roomCode = code.value.trim().toUpperCase();
    if (!/^[A-HJ-NP-Z2-9]{6}$/.test(roomCode)) {
      loadedCode = "";
      genres.replaceChildren();
      genreStatus.textContent = "Enter a valid room code to load genre choices.";
      return;
    }
    const selected = selectedGenres(genres);
    genreStatus.textContent = "Loading room genres…";
    try {
      const result = await api(
        `/api/rooms/${encodeURIComponent(roomCode)}/genres`,
      );
      loadedCode = roomCode;
      renderGenreChoices(genres, result.genres || [], selected);
      genreStatus.textContent = result.genres?.length
        ? `${result.genres.length} genres available.`
        : "This room has no genre metadata; you’ll see every title.";
    } catch (requestError) {
      loadedCode = "";
      genres.replaceChildren();
      genreStatus.textContent = requestError.message;
    }
  };
  code.addEventListener("input", () => {
    code.value = code.value.toUpperCase();
    clearTimeout(genreTimer);
    genreTimer = setTimeout(loadGenres, 220);
  });
  if (initialCode) void loadGenres();

  form.onsubmit = async (event) => {
    event.preventDefault();
    error.textContent = "";
    submit.disabled = true;
    try {
      const roomCode = code.value.trim().toUpperCase();
      if (loadedCode !== roomCode) await loadGenres();
      const joined = await api("/api/rooms/join", {
        method: "POST",
        body: JSON.stringify({
          code: roomCode,
          name: name.value,
          genres: selectedGenres(genres),
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
