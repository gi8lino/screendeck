import { api } from "./api.js";
import { renderCreateRoom } from "./create-room.js";
import { renderJoinRoom } from "./join-room.js";
import { renderPlexSetup } from "./plex.js";
import { renderRoom, stopRoomEvents } from "./room.js";
import { getConfig, getSession, setConfig } from "./state.js";
import { el, root, topbar } from "./ui.js";

const navigation = {
  renderHome,
  renderJoinRoom: (roomCode = "") => renderJoinRoom(navigation, roomCode),
  renderRoom: () => renderRoom(navigation),
};

// renderHome displays the ScreenDeck landing page.
function renderHome() {
  stopRoomEvents();
  root.replaceChildren();
  root.append(topbar());
  const config = getConfig();
  const hero = el("section", "hero");
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
}

// invitedRoomCode returns a valid room code from the current share URL.
function invitedRoomCode() {
  const code = new URLSearchParams(window.location.search)
    .get("room")
    ?.trim()
    .toUpperCase();
  return /^[A-HJ-NP-Z2-9]{6}$/.test(code || "") ? code : "";
}

// boot loads public configuration and restores an active room session.
async function boot() {
  try {
    setConfig(await api("/api/config"));
    const roomCode = invitedRoomCode();
    const session = getSession();
    if (roomCode && session?.code !== roomCode) {
      navigation.renderJoinRoom(roomCode);
    } else if (session) {
      await renderRoom(navigation);
    } else if (roomCode) {
      navigation.renderJoinRoom(roomCode);
    } else {
      renderHome();
    }
  } catch (error) {
    root.replaceChildren(
      el("div", "notice", `ScreenDeck could not start: ${error.message}`),
    );
  }
}

await boot();
