import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import { backButton, el, root, showError, topbar } from "./ui.js";

// renderJoinRoom displays the form for joining an existing room.
export function renderJoinRoom(navigation, initialCode = "") {
  const view = createJoinRoomView(navigation, initialCode);
  const state = { genreTimer: undefined, loadedCode: "" };
  const loadGenres = async () => {
    state.loadedCode = await loadJoinRoomGenres(
      view.code,
      view.genres,
      view.genreStatus,
    );
  };

  view.code.addEventListener("input", () => {
    view.code.value = view.code.value.toUpperCase();
    clearTimeout(state.genreTimer);
    state.genreTimer = setTimeout(loadGenres, 220);
  });
  if (initialCode) void loadGenres();

  view.form.onsubmit = (event) =>
    submitJoinRoom(event, navigation, view, state, loadGenres);
}

// submitJoinRoom joins the requested room with the participant's current preferences.
async function submitJoinRoom(event, navigation, view, state, loadGenres) {
  event.preventDefault();
  view.error.textContent = "";
  view.submit.disabled = true;

  try {
    const roomCode = normalizedRoomCode(view.code.value);
    if (state.loadedCode !== roomCode) await loadGenres();

    const joined = await api("/api/rooms/join", {
      method: "POST",
      body: JSON.stringify({
        code: roomCode,
        name: view.name.value,
        genres: selectedGenres(view.genres),
        genreMode: view.genreMode.value,
      }),
    });
    saveSession(joined);
    await navigation.renderRoom();
  } catch (requestError) {
    showError(view.error, requestError);
    view.submit.disabled = false;
  }
}

// createJoinRoomView builds the static join form and returns its interactive controls.
function createJoinRoomView(navigation, initialCode) {
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

  const genreFields = createJoinGenreFields();
  const error = el("p", "error");
  const submit = el("button", "btn primary", "Join room");
  submit.type = "submit";
  form.append(codeRow, nameRow, genreFields.box, error, submit);
  panel.append(form);
  root.append(panel);

  if (initialCode) name.focus();
  return {
    form,
    code,
    name,
    genreMode: genreFields.genreMode,
    genreStatus: genreFields.status,
    genres: genreFields.genres,
    error,
    submit,
  };
}

// createJoinGenreFields creates personal genre controls for a joining participant.
function createJoinGenreFields() {
  const box = el("section", "filter-box preference-box");
  box.append(
    el("div", "label", "Your genres"),
    el(
      "p",
      "muted",
      "Optional · pick what you personally want to swipe. Leave empty to see every title in the room.",
    ),
  );

  const genreMode = el("select");
  const anyMode = el("option", "", "Match any selected genre");
  anyMode.value = "any";
  const allMode = el("option", "", "Match all selected genres");
  allMode.value = "all";
  genreMode.append(anyMode, allMode);

  const status = el(
    "p",
    "muted",
    "Enter a valid room code to load genre choices.",
  );
  const genres = el("div", "genre-chips");
  genres.setAttribute("role", "group");
  genres.setAttribute("aria-label", "Your genres");
  box.append(genreMode, status, genres);
  return { box, genreMode, status, genres };
}

// loadJoinRoomGenres refreshes genre choices for the current room code.
async function loadJoinRoomGenres(codeInput, genres, status) {
  const roomCode = normalizedRoomCode(codeInput.value);
  if (!validRoomCode(roomCode)) {
    genres.replaceChildren();
    status.textContent = "Enter a valid room code to load genre choices.";
    return "";
  }

  const selected = selectedGenres(genres);
  status.textContent = "Loading room genres…";
  try {
    const result = await api(
      `/api/rooms/${encodeURIComponent(roomCode)}/genres`,
    );
    renderGenreChoices(genres, result.genres || [], selected);
    status.textContent = result.genres?.length
      ? `${result.genres.length} genres available.`
      : "This room has no genre metadata; you’ll see every title.";
    return roomCode;
  } catch (requestError) {
    genres.replaceChildren();
    status.textContent = requestError.message;
    return "";
  }
}

// normalizedRoomCode canonicalizes room-code input for API requests.
function normalizedRoomCode(value) {
  return value.trim().toUpperCase();
}

// validRoomCode reports whether a room code uses the ScreenDeck code alphabet.
function validRoomCode(code) {
  return /^[A-HJ-NP-Z2-9]{6}$/.test(code);
}
