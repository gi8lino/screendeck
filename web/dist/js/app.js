import { api } from "./api.js";
import { renderCreateRoom } from "./create-room.js";
import { renderJoinRoom } from "./join-room.js";
import { renderPlexSetup } from "./plex.js";
import { renderRoom, stopRoomEvents } from "./room.js";
import { getConfig, getSession, saveSession, setConfig } from "./state.js";
import { el, root, showToast, topbar, updateFooter } from "./ui.js";

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
  root.replaceChildren();
  root.append(topbar());

  const config = getConfig();
  const hero = el("section", "hero home-hero");
  hero.append(
    el("div", "eyebrow", "Tonight, decided."),
    el("h1", "", "Stop scrolling. Start watching."),
    el(
      "p",
      "lede",
      "Invite your people, swipe through movies and TV shows from Plex, and find what everyone actually wants to watch.",
    ),
  );
  if (!config.plexConfigured) {
    hero.append(
      el(
        "div",
        "notice",
        "Connect a Plex account to choose a server and load its movie and TV libraries. Credentials stay encrypted on this server.",
      ),
    );
  }
  const actions = el("div", "actions");
  const create = el(
    "button",
    "btn primary",
    config.plexConfigured ? "Create a room" : "Connect Plex",
  );
  create.onclick = config.plexConfigured
    ? () => renderCreateRoom(navigation)
    : () => renderPlexSetup(navigation);
  const join = el("button", "btn ghost", "Join friends");
  join.onclick = () => navigation.renderJoinRoom();
  actions.append(create, join);
  hero.append(actions);
  root.append(hero);

  const rooms = el("section", "saved-rooms");
  rooms.append(
    savedRoomsHeader(),
    el("div", "empty saved-rooms-loading", "Loading your rooms…"),
  );
  root.append(rooms);
  void loadSavedRooms(rooms);
}

// savedRoomsHeader creates the heading shown above persistent room memberships.
function savedRoomsHeader() {
  const header = el("div", "saved-rooms-head");
  const copy = el("div");
  copy.append(
    el("div", "eyebrow", "Your rooms"),
    el("h2", "", "Pick up where you left off."),
  );
  header.append(copy);
  return header;
}

// loadSavedRooms fetches and renders room memberships for the current browser identity.
async function loadSavedRooms(section) {
  try {
    const result = await api("/api/me/rooms");
    const rooms = Array.isArray(result) ? result : [];
    if (rooms.length === 0) {
      section.replaceChildren(
        savedRoomsHeader(),
        el(
          "div",
          "empty saved-rooms-empty",
          "Rooms you create or join on this browser will appear here.",
        ),
      );
      return;
    }

    renderSavedRooms(section, rooms);
  } catch (error) {
    section.replaceChildren(
      savedRoomsHeader(),
      el("div", "notice", `Could not load your rooms: ${error.message}`),
    );
  }
}

// renderSavedRooms renders a bounded room preview and an optional expansion control.
function renderSavedRooms(section, rooms, expanded = false) {
  section.replaceChildren(savedRoomsHeader());

  const visibleRooms = expanded
    ? rooms
    : rooms.slice(0, savedRoomsPreviewLimit);
  const list = el("div", "saved-room-list");
  visibleRooms.forEach((room) => list.append(savedRoomCard(room)));
  section.append(list);

  if (rooms.length <= savedRoomsPreviewLimit) return;

  const toggle = el(
    "button",
    "saved-rooms-toggle",
    expanded ? "Show fewer rooms ↑" : `Show all rooms (${rooms.length}) ↓`,
  );
  toggle.type = "button";
  toggle.setAttribute("aria-expanded", String(expanded));
  toggle.onclick = () => renderSavedRooms(section, rooms, !expanded);
  section.append(toggle);
}

// savedRoomCard creates one resumable room membership card.
function savedRoomCard(room) {
  const card = el("button", "saved-room");
  card.type = "button";
  card.setAttribute("aria-label", `Open room ${room.code}`);

  const main = el("span", "saved-room-main");
  const heading = el("span", "saved-room-heading");
  heading.append(
    el("strong", "saved-room-code", room.code),
    el(
      "span",
      "saved-room-role",
      room.isHost ? `${room.name} · host` : room.name,
    ),
  );
  main.append(
    heading,
    el(
      "span",
      "saved-room-meta",
      `${roomPhaseLabel(room.phase)} · Round ${room.round} · ${participantLabel(room.participantCount)}`,
    ),
  );

  const open = el("span", "saved-room-open", "Open →");
  card.append(main, open);
  card.onclick = async () => {
    if (card.disabled) return;
    card.disabled = true;
    try {
      await resumeRoom(room.code);
    } catch (error) {
      showToast(error.message);
      card.disabled = false;
      void loadSavedRooms(card.closest(".saved-rooms"));
    }
  };
  return card;
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
      el("div", "notice", `ScreenDeck could not start: ${error.message}`),
    );
  }
}

await boot();
