import { api } from "./api.js";
import { renderCreateRoom } from "./create-room.js";
import { renderJoinRoom } from "./join-room.js";
import { renderPlexSetup } from "./plex.js";
import { renderRoom, stopRoomEvents } from "./room.js";
import { getConfig, getSession, saveSession, setConfig } from "./state.js";
import {
  instantiateTemplate,
  messageElement,
  root,
  showToast,
  templateElement,
  topbar,
  updateFooter,
} from "./ui.js";

const navigation = {
  renderHome,
  renderCreateRoom: () => renderCreateRoom(navigation),
  renderJoinRoom: (roomCode = "") => renderJoinRoom(navigation, roomCode),
  renderRoom: () => renderRoom(navigation),
};

const savedRoomsPreviewLimit = 5;

// renderHome displays the ScreenDeck landing page and persistent room memberships.
function renderHome() {
  stopRoomEvents();
  const config = getConfig();
  const { fragment, refs } = instantiateTemplate("home-template");

  refs.plexNotice.hidden = config.plexConfigured;
  refs.primaryAction.textContent = config.plexConfigured
    ? "Create a room"
    : "Connect Plex";
  refs.primaryAction.onclick = config.plexConfigured
    ? () => renderCreateRoom(navigation)
    : () => renderPlexSetup(navigation);
  refs.joinAction.onclick = () => navigation.renderJoinRoom();

  root.replaceChildren(topbar(), fragment);
  void loadSavedRooms(refs.savedRooms);
}

// savedRoomsHeader clones the shared static heading for persistent room memberships.
function savedRoomsHeader() {
  return instantiateTemplate("saved-rooms-header-template").fragment;
}

// loadSavedRooms fetches and renders room memberships for the current browser identity.
async function loadSavedRooms(section) {
  try {
    const result = await api("/api/me/rooms");
    const rooms = Array.isArray(result) ? result : [];
    if (rooms.length === 0) {
      const empty = messageElement(
        "empty-template",
        "Rooms you create or join on this browser will appear here.",
      );
      empty.classList.add("saved-rooms-empty");
      section.replaceChildren(savedRoomsHeader(), empty);
      return;
    }

    renderSavedRooms(section, rooms);
  } catch (error) {
    section.replaceChildren(
      savedRoomsHeader(),
      messageElement(
        "notice-template",
        `Could not load your rooms: ${error.message}`,
      ),
    );
  }
}

// renderSavedRooms renders a bounded room preview and an optional expansion control.
function renderSavedRooms(section, rooms, expanded = false) {
  section.replaceChildren(savedRoomsHeader());

  const visibleRooms = expanded
    ? rooms
    : rooms.slice(0, savedRoomsPreviewLimit);
  const { element: list } = templateElement("saved-room-list-template");
  visibleRooms.forEach((room) => list.append(savedRoomCard(room)));
  section.append(list);

  if (rooms.length <= savedRoomsPreviewLimit) return;

  const { element: toggle } = templateElement("saved-rooms-toggle-template");
  toggle.textContent = expanded
    ? "Show fewer rooms ↑"
    : `Show all rooms (${rooms.length}) ↓`;
  toggle.setAttribute("aria-expanded", String(expanded));
  toggle.onclick = () => renderSavedRooms(section, rooms, !expanded);
  section.append(toggle);
}

// savedRoomCard creates one resumable room membership card.
function savedRoomCard(room) {
  const { element: card, refs } = templateElement("saved-room-template");
  card.setAttribute("aria-label", `Open room ${room.code}`);
  refs.code.textContent = room.code;
  refs.role.textContent = room.isHost ? `${room.name} · host` : room.name;
  refs.meta.textContent = `${roomPhaseLabel(room.phase)} · Round ${room.round} · ${participantLabel(room.participantCount)}`;
  card.onclick = () => openSavedRoom(card, room.code);
  return card;
}

// openSavedRoom resumes one membership and restores the card after failures.
async function openSavedRoom(card, roomCode) {
  if (card.disabled) return;
  card.disabled = true;

  try {
    await resumeRoom(roomCode);
  } catch (error) {
    showToast(error.message);
    card.disabled = false;
    void loadSavedRooms(card.closest(".saved-rooms"));
  }
}

// participantLabel formats a room participant count for display.
function participantLabel(count) {
  const total = Number(count) || 0;
  return `${total} ${total === 1 ? "person" : "people"}`;
}

// roomPhaseLabel converts persisted room phases into concise home-screen labels.
function roomPhaseLabel(phase) {
  switch (phase) {
    case "next_round_requested":
      return "Next round requested";
    case "round_complete":
      return "Round complete";
    case "finished":
      return "Finished";
    default:
      return "Swiping";
  }
}

// resumeRoom restores a saved participant session and opens the selected room.
async function resumeRoom(roomCode) {
  const session = await api(
    `/api/me/rooms/${encodeURIComponent(roomCode)}/session`,
    { method: "POST" },
  );
  saveSession(session);
  await navigation.renderRoom();
}

// tryResumeRoom opens a room when the current browser already owns a membership for it.
async function tryResumeRoom(roomCode) {
  try {
    await resumeRoom(roomCode);
    return true;
  } catch (error) {
    if (error.status === 404) return false;
    throw error;
  }
}

// invitedRoomCode returns a valid room code from the current share URL.
function invitedRoomCode() {
  const code = new URLSearchParams(window.location.search)
    .get("room")
    ?.trim()
    .toUpperCase();
  return /^[A-HJ-NP-Z2-9]{6}$/.test(code || "") ? code : "";
}

// boot loads public configuration and restores an active or persisted room session.
async function boot() {
  try {
    setConfig(await api("/api/config"));
    updateFooter(getConfig());
    const roomCode = invitedRoomCode();
    const session = getSession();
    if (roomCode && session?.code === roomCode) {
      await renderRoom(navigation);
      return;
    }
    if (roomCode && (await tryResumeRoom(roomCode))) return;
    if (roomCode) {
      navigation.renderJoinRoom(roomCode);
      return;
    }
    // Normal startup always shows the room picker. Opening a saved room explicitly
    // restores the exact participant session for that browser identity.
    renderHome();
  } catch (error) {
    updateFooter();
    root.replaceChildren(
      messageElement(
        "notice-template",
        `ScreenDeck could not start: ${error.message}`,
      ),
    );
  }
}

await boot();
