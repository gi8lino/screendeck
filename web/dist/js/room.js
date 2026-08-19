import { api } from "./api.js";
import { getConfig, getSession, saveSession } from "./state.js";
import { el, root, showToast, topbar } from "./ui.js";

let eventSource;
let voting = false;
let navigation;

// renderRoom loads and displays the current room.
export async function renderRoom(nextNavigation) {
  if (nextNavigation) navigation = nextNavigation;
  const session = getSession();
  if (!session) return navigation.renderHome();
  try {
    const state = await api(`/api/rooms/${encodeURIComponent(session.code)}`);
    drawRoom(state);
    connectEvents();
  } catch (error) {
    saveSession(null);
    navigation.renderHome();
    showToast(error.message);
  }
}

// stopRoomEvents closes the active room event stream.
export function stopRoomEvents() {
  eventSource?.close();
  eventSource = null;
}

// drawRoom renders participants, the current candidate, and matches.
function drawRoom(state) {
  const session = getSession();
  root.replaceChildren();
  const leave = el("button", "btn ghost", "Leave");
  leave.onclick = async () => {
    try {
      await api(`/api/rooms/${encodeURIComponent(session.code)}`, {
        method: "DELETE",
      });
    } catch {
      /* session may already be gone */
    }
    stopRoomEvents();
    saveSession(null);
    navigation.renderHome();
  };
  root.append(topbar(leave));
  const head = el("section", "room-head");
  const intro = el("div");
  intro.append(
    el("div", "eyebrow", "Swipe room"),
    el("h2", "", `Good hunting, ${state.me.name}.`),
  );
  const people = el("div", "people");
  state.participants.forEach((participant) =>
    people.append(
      el(
        "span",
        `person${participant.id === state.me.id ? " me" : ""}`,
        `${participant.name}${participant.id === state.me.id ? " · you" : ""}`,
      ),
    ),
  );
  intro.append(people);
  const code = el("button", "room-code", state.room.code);
  code.title = "Copy room link";
  code.onclick = async () => {
    try {
      await navigator.clipboard.writeText(roomURL(state.room.code));
      showToast("Room link copied");
    } catch {
      showToast("Could not copy room link");
    }
  };
  head.append(intro, code);
  root.append(head);

  const grid = el("section", "room-grid");
  const left = el("div");
  if (state.candidate) {
    const showDetails = () => showMovieDetails(state.candidate);
    const deck = el("div", "deck");
    const card = movieCard(state.candidate, showDetails);
    deck.append(card);
    left.append(deck);
    const actions = el("div", "swipe-actions");
    const no = el("button", "btn icon no", "×");
    no.title = "Pass";
    no.onclick = () => vote(state.candidate.id, false, card);
    const details = el("button", "btn icon info", "i");
    details.title = "View details";
    details.setAttribute("aria-label", "View title details");
    details.onclick = showDetails;
    const yes = el("button", "btn icon yes", "♥");
    yes.title = "Like";
    yes.onclick = () => vote(state.candidate.id, true, card);
    actions.append(no, details, yes);
    left.append(actions);
    enableSwipe(card, state.candidate.id);
  } else {
    const done = el("div", "finished");
    done.append(
      el("div", "eyebrow", "That’s the lot"),
      el("h2", "", "You’ve seen every title."),
      el("p", "lede", "Hang tight while everyone else finishes swiping."),
    );
    left.append(done);
  }
  left.append(
    el(
      "div",
      "progress",
      `${state.progress.voted} of ${state.progress.total} considered`,
    ),
  );

  const side = el("aside", "side");
  side.append(el("h3", "", `Matches · ${(state.matches || []).length}`));
  const list = el("div", "match-list");
  if (!state.matches?.length) {
    list.append(
      el(
        "div",
        "empty",
        state.participants.length < 2
          ? "Invite someone with the room code. Matches need at least two people."
          : "A shared yes will appear here.",
      ),
    );
  }
  (state.matches || []).forEach((movie) => {
    const figure = el("figure", "match");
    const image = el("img");
    image.src = `/api/posters/${encodeURIComponent(movie.id)}`;
    image.alt = "";
    figure.append(image, el("figcaption", "", movie.title));
    list.append(figure);
  });
  side.append(list);
  grid.append(left, side);
  root.append(grid);
}

// roomURL builds a direct join URL for a room code.
function roomURL(roomCode) {
  const url = new URL(getConfig().baseUrl || window.location.origin);
  url.search = "";
  url.hash = "";
  url.searchParams.set("room", roomCode);
  return url.toString();
}

// movieCard builds a swipeable card for one movie or TV show.
function movieCard(movie, showDetails) {
  const card = el("article", "card");
  card.tabIndex = 0;
  card.setAttribute("role", "button");
  card.setAttribute("aria-label", `View details for ${movie.title}`);
  card.title = "View details";
  card.onclick = showDetails;
  card.onkeydown = (event) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    showDetails();
  };
  const image = el("img", "poster");
  image.src = `/api/posters/${encodeURIComponent(movie.id)}`;
  image.alt = `Poster for ${movie.title}`;
  const nope = el("div", "stamp nope", "NOPE");
  const like = el("div", "stamp like", "LIKE");
  const body = el("div", "card-body");
  body.append(el("h2", "movie-title", movie.title));
  const meta = el("div", "meta");
  const bits = [
    movie.type === "show" ? "TV series" : "Movie",
    movie.year || null,
    movie.type === "movie" && movie.duration
      ? `${Math.round(movie.duration / 60000)} min`
      : null,
    movie.rating ? `★ ${movie.rating.toFixed(1)}` : null,
  ].filter(Boolean);
  meta.textContent = bits.join("  ·  ");
  body.append(
    meta,
    el(
      "p",
      "summary",
      movie.summary || movie.genres?.join(" · ") || "No synopsis available.",
    ),
  );
  card.append(image, nope, like, body);
  return card;
}

// showMovieDetails opens the complete metadata and synopsis for a title.
function showMovieDetails(movie) {
  document.querySelector(".movie-dialog")?.remove();
  const dialog = el("dialog", "movie-dialog");
  const close = el("button", "dialog-close", "×");
  close.type = "button";
  close.setAttribute("aria-label", "Close details");
  close.onclick = () => dialog.close();
  const image = el("img", "dialog-poster");
  image.src = `/api/posters/${encodeURIComponent(movie.id)}`;
  image.alt = `Poster for ${movie.title}`;
  const content = el("div", "dialog-content");
  content.append(
    el("div", "eyebrow", movie.type === "show" ? "TV series" : "Movie"),
    el("h2", "", movie.title),
    el("div", "meta", movieMetadata(movie).join("  ·  ")),
    el("p", "dialog-summary", movie.summary || "No synopsis available."),
  );
  if (movie.genres?.length) {
    content.append(el("p", "dialog-genres", movie.genres.join(" · ")));
  }
  dialog.append(close, image, content);
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
  dialog.addEventListener("close", () => dialog.remove(), { once: true });
  document.body.append(dialog);
  dialog.showModal();
}

// movieMetadata returns display-ready metadata for a movie or TV show.
function movieMetadata(movie) {
  return [
    movie.year || null,
    movie.type === "movie" && movie.duration
      ? `${Math.round(movie.duration / 60000)} min`
      : null,
    movie.rating ? `★ ${movie.rating.toFixed(1)}` : null,
  ].filter(Boolean);
}

// enableSwipe adds pointer-driven voting gestures to a card.
function enableSwipe(card, movieID) {
  let start = 0;
  let delta = 0;
  let active = false;
  let suppressClick = false;
  card.onpointerdown = (event) => {
    active = true;
    start = event.clientX;
    delta = 0;
    suppressClick = false;
    card.setPointerCapture(event.pointerId);
  };
  card.onpointermove = (event) => {
    if (!active) return;
    delta = event.clientX - start;
    if (Math.abs(delta) > 6) suppressClick = true;
    card.style.transition = "none";
    card.style.transform = `translateX(${delta}px) rotate(${delta / 18}deg)`;
    card.querySelector(delta > 0 ? ".like" : ".nope").style.opacity = Math.min(
      Math.abs(delta) / 100,
      1,
    );
  };
  card.onpointerup = () => {
    if (!active) return;
    active = false;
    if (Math.abs(delta) > 90) {
      vote(movieID, delta > 0, card);
      return;
    }
    card.style.transition = "";
    card.style.transform = "";
    card
      .querySelectorAll(".stamp")
      .forEach((stamp) => (stamp.style.opacity = 0));
  };
  card.addEventListener(
    "click",
    (event) => {
      if (!suppressClick) return;
      event.stopImmediatePropagation();
      suppressClick = false;
    },
    { capture: true },
  );
}

// vote records a choice and advances to the next candidate.
async function vote(movieID, liked, card) {
  if (voting) return;
  const session = getSession();
  voting = true;
  card.style.transition = "transform .28s ease, opacity .28s";
  card.style.transform = `translateX(${liked ? 130 : -130}%) rotate(${liked ? 16 : -16}deg)`;
  card.style.opacity = 0;
  try {
    const result = await api(
      `/api/rooms/${encodeURIComponent(session.code)}/votes`,
      { method: "POST", body: JSON.stringify({ movieId: movieID, liked }) },
    );
    if (result.matched) showToast("It’s a match!");
    await renderRoom();
  } catch (error) {
    showToast(error.message);
    card.style.transform = "";
    card.style.opacity = 1;
  } finally {
    voting = false;
  }
}

// connectEvents subscribes to changes from other room participants.
function connectEvents() {
  const session = getSession();
  if (!session || eventSource) return;
  eventSource = new EventSource(
    `/api/rooms/${encodeURIComponent(session.code)}/events?token=${encodeURIComponent(session.token)}`,
  );
  eventSource.addEventListener("update", (event) => {
    if (event.data === "changed" && !voting) renderRoom();
  });
  eventSource.onerror = () => {
    stopRoomEvents();
    setTimeout(connectEvents, 3000);
  };
}
