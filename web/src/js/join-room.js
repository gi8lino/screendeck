import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import {
  backButton,
  instantiateTemplate,
  root,
  showError,
  topbar,
} from "./ui.js";

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

// createJoinRoomView clones the static join form and returns its controls.
function createJoinRoomView(navigation, initialCode) {
  const { fragment, refs } = instantiateTemplate("join-room-template");
  refs.code.value = initialCode;

  root.replaceChildren(topbar(backButton(navigation.renderHome)), fragment);

  if (initialCode) refs.name.focus();
  return refs;
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
