import { api } from "./api.js";
import { getConfig, getSession, saveSession } from "./state.js";
import { el, root, showToast, topbar } from "./ui.js";

let eventSource;
let voting = false;
let navigation;
let trackedRoomCode = "";
let trackedRound = 0;
let knownMatchIDs = new Set();
let matchQueue = [];
let matchDialogOpen = false;

// renderRoom loads and displays the current room.
export async function renderRoom(nextNavigation) {
  if (nextNavigation) navigation = nextNavigation;
  const session = getSession();
  if (!session) return navigation.renderHome();
  try {
    const state = await api(`/api/rooms/${encodeURIComponent(session.code)}`);
    drawRoom(state);
    trackMatches(state);
    connectEvents();
  } catch (error) {
    resetMatchTracking();
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

// drawRoom renders participants, the current candidate, matches, and next-round readiness.
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
    resetMatchTracking();
    saveSession(null);
    navigation.renderHome();
  };
  root.append(topbar(leave));

  const head = el("section", "room-head");
  const intro = el("div");
  intro.append(
    el("div", "eyebrow", `Round ${state.room.round} · ${phaseLabel(state.room.phase)}`),
    el("h2", "", `Good hunting, ${state.me.name}.`),
  );
  const people = el("div", "people");
  state.participants.forEach((participant) => {
    const labels = [participant.name];
    if (participant.id === state.me.id) labels.push("you");
    if (participant.readyForNextRound) labels.push("next round ✓");
    const person = el(
      "span",
      `person${participant.id === state.me.id ? " me" : ""}${participant.readyForNextRound ? " ready" : ""}`,
      labels.join(" · "),
    );
    person.title = participant.genres?.length
      ? `Genres: ${participant.genres.join(", ")}`
      : "Genres: everything";
    people.append(person);
  });
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
    no.onclick = () => vote(state.candidate, false, card);
    const details = el("button", "btn icon info", "i");
    details.title = "View details";
    details.setAttribute("aria-label", "View title details");
    details.onclick = showDetails;
    const yes = el("button", "btn icon yes", "♥");
    yes.title = "Like";
    yes.onclick = () => vote(state.candidate, true, card);
    actions.append(no, details, yes);
    left.append(actions);
    enableSwipe(card, state.candidate);
  } else {
    left.append(finishedCard(state));
  }
  left.append(
    el(
      "div",
      "progress",
      `${state.progress.voted} of ${state.progress.total} considered · round ${state.room.round}`,
    ),
  );

  const side = el("aside", "side");
  side.append(
    el("h3", "", `Round ${state.room.round} matches · ${(state.matches || []).length}`),
    matchSummary(state),
  );
  const moreTitles = moreTitlesPanel(state);
  if (moreTitles) side.append(moreTitles);
  const nextRound = nextRoundPanel(state);
  if (nextRound) side.append(nextRound);

  grid.append(left, side);
  root.append(grid);
}


// phaseLabel converts the persisted room state machine phase into UI copy.
function phaseLabel(phase) {
  switch (phase) {
    case "next_round_requested":
      return "next round requested";
    case "round_complete":
      return "round complete";
    case "finished":
      return "winner found";
    default:
      return "swiping";
  }
}

// matchSummary renders a compact stack instead of every matched poster.
function matchSummary(state) {
  const matches = state.matches || [];
  if (!matches.length) {
    return el(
      "div",
      "empty",
      state.participants.length < 2
        ? "Invite someone with the room code. Matches need at least two people."
        : "A shared yes will appear here.",
    );
  }

  const pile = el("button", "match-pile");
  pile.type = "button";
  pile.setAttribute(
    "aria-label",
    `Show ${matches.length} ${matches.length === 1 ? "match" : "matches"}`,
  );
  pile.onclick = () => showMatches(matches, state.room.round);

  const stack = el("span", "match-pile-stack");
  matches.slice(0, 3).forEach((movie) => {
    const image = el("img", "match-pile-poster");
    image.src = `/api/posters/${encodeURIComponent(movie.id)}`;
    image.alt = "";
    stack.append(image);
  });
  const count = el("span", "match-pile-count", String(matches.length));
  stack.append(count);

  const label = el("span", "match-pile-label");
  label.append(
    el(
      "strong",
      "",
      `${matches.length} ${matches.length === 1 ? "match" : "matches"}`,
    ),
    el("small", "", "Click to open the pile"),
  );
  pile.append(stack, label);
  return pile;
}

// showMatches expands the current match pile in a scrollable dialog.
function showMatches(matches, round) {
  document.querySelector(".matches-dialog")?.remove();
  const dialog = el("dialog", "matches-dialog");
  const close = el("button", "dialog-close", "×");
  close.type = "button";
  close.setAttribute("aria-label", "Close matches");
  close.onclick = () => dialog.close();

  const header = el("div", "matches-dialog-head");
  header.append(
    el("div", "eyebrow", `Round ${round}`),
    el("h2", "", `${matches.length} ${matches.length === 1 ? "match" : "matches"}`),
    el("p", "muted", "These are the titles everyone has liked so far."),
  );

  const list = el("div", "matches-dialog-grid");
  matches.forEach((movie) => {
    const button = el("button", "match-grid-item");
    button.type = "button";
    button.title = `View details for ${movie.title}`;
    const image = el("img");
    image.src = `/api/posters/${encodeURIComponent(movie.id)}`;
    image.alt = `Poster for ${movie.title}`;
    button.append(image, el("span", "", movie.title));
    button.onclick = () => {
      dialog.close();
      showMovieDetails(movie);
    };
    list.append(button);
  });

  dialog.append(close, header, list);
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
  dialog.addEventListener(
    "close",
    () => {
      dialog.remove();
      showNextMatch();
    },
    { once: true },
  );
  document.body.append(dialog);
  dialog.showModal();
}

// moreTitlesPanel lets the room expand the first round from its unused reserve.
function moreTitlesPanel(state) {
  const available = state.moreTitles?.available || 0;
  if (state.room.round !== 1 || available <= 0) return null;

  const panel = el("section", "more-titles-panel");
  panel.append(
    el("h3", "", "Need more options?"),
    el("p", "muted", `${available} unused titles remain from the original filtered pool.`),
  );
  const actions = el("div", "more-titles-actions");
  [50, 100, 250].forEach((count) => {
    const amount = Math.min(count, available);
    if (amount <= 0) return;
    const button = el("button", "btn ghost compact-button", `+${amount}`);
    button.type = "button";
    button.onclick = () => addMoreTitles(amount, button);
    actions.append(button);
  });
  panel.append(actions);
  return panel;
}

// addMoreTitles activates more unseen titles in the first round.
async function addMoreTitles(count, button) {
  if (button.disabled) return;
  const session = getSession();
  button.disabled = true;
  try {
    const result = await api(
      `/api/rooms/${encodeURIComponent(session.code)}/more-titles`,
      { method: "POST", body: JSON.stringify({ count }) },
    );
    showToast(`Added ${result.added} titles · ${result.remaining} remain`);
    await renderRoom();
  } catch (error) {
    showToast(error.message);
    button.disabled = false;
  }
}

// nextRoundPanel renders the unanimous agreement flow as soon as two matches exist.
function nextRoundPanel(state) {
  if (state.participants.length < 2) return null;

  const matches = state.matches || [];
  const readiness = state.nextRound || {
    ready: 0,
    required: state.participants.length,
    available: false,
  };
  if (matches.length < 2 && readiness.ready === 0) return null;

  const panel = el("section", "next-round-panel");
  panel.append(el("h3", "", "Next round"));

  if (readiness.ready > 0) {
    const requester = readiness.requestedBy;
    panel.append(
      el(
        "p",
        "next-round-status",
        requester
          ? `${requester.id === state.me.id ? "You" : requester.name} asked for the next round.`
          : "A next round was requested.",
      ),
      el("p", "muted", `${readiness.ready} of ${readiness.required} ready`),
    );
    const roster = el("div", "next-round-roster");
    state.participants.forEach((participant) => {
      const ready = participant.readyForNextRound;
      roster.append(
        el(
          "div",
          `next-round-person${ready ? " ready" : ""}`,
          `${ready ? "✓" : "○"} ${participant.name}${participant.id === state.me.id ? " · you" : ""}`,
        ),
      );
    });
    panel.append(roster);
  } else {
    panel.append(
      el(
        "p",
        "muted",
        "Ask everyone to narrow the deck to the matches you have right now. Readiness resets if the group changes or fewer than two matches remain.",
      ),
    );
  }

  if (matches.length < 2 && state.me.readyForNextRound) {
    panel.append(
      el(
        "p",
        "muted",
        "There are fewer than two matches now. Withdraw your readiness or keep swiping until another match appears.",
      ),
    );
  }

  if (matches.length >= 2 || state.me.readyForNextRound) {
    const button = el(
      "button",
      state.me.readyForNextRound ? "btn ghost next-round-button" : "btn primary next-round-button",
      state.me.readyForNextRound
        ? "Withdraw readiness"
        : readiness.ready > 0
          ? "Ready for next round"
          : "Ask for next round",
    );
    button.type = "button";
    button.onclick = () => toggleNextRoundReady(state, button);
    panel.append(button);
  }
  return panel;
}

// toggleNextRoundReady records or withdraws this participant's agreement.
async function toggleNextRoundReady(state, button) {
  if (button.disabled) return;
  const session = getSession();
  const ready = !state.me.readyForNextRound;
  button.disabled = true;
  button.textContent = ready ? "Marking ready…" : "Withdrawing…";
  try {
    const result = await api(
      `/api/rooms/${encodeURIComponent(session.code)}/round-ready`,
      {
        method: "POST",
        body: JSON.stringify({ round: state.room.round, ready }),
      },
    );
    if (result.advanced) {
      showToast(`Round ${result.round}: ${result.titles} matched titles`);
    } else {
      showToast(`${result.ready} of ${result.required} ready`);
    }
    await renderRoom();
  } catch (error) {
    showToast(error.message);
    button.disabled = false;
    button.textContent = state.me.readyForNextRound
      ? "Withdraw readiness"
      : state.nextRound?.ready > 0
        ? "Ready for next round"
        : "Ask for next round";
  }
}

// finishedCard renders the state shown after this participant exhausts their personal deck.
function finishedCard(state) {
  const done = el("div", "finished");
  const matches = state.matches || [];

  if (state.roundComplete && matches.length === 1) {
    done.classList.add("winner");
    done.append(
      el("div", "eyebrow", "Decision made"),
      el("h2", "", "One title remains."),
      el("p", "lede", `${matches[0].title} survived the complete round.`),
    );
    return done;
  }

  if (state.roundComplete && matches.length === 0) {
    done.append(
      el("div", "eyebrow", "Round complete"),
      el("h2", "", "No shared pick survived."),
      el(
        "p",
        "lede",
        "Start a new room with a wider deck or different filters to try again.",
      ),
    );
    return done;
  }

  if (matches.length > 1) {
    done.append(
      el("div", "eyebrow", "Your deck is complete"),
      el("h2", "", `${matches.length} matches so far.`),
      el(
        "p",
        "lede",
        "You can wait for more matches or use the next-round request to narrow the group now.",
      ),
    );
    return done;
  }

  done.append(
    el("div", "eyebrow", "You’re done for now"),
    el("h2", "", "You’ve seen your whole deck."),
    el(
      "p",
      "lede",
      state.roundComplete
        ? "The group has finished this deck."
        : "Other participants can keep swiping; new matches will appear live.",
    ),
  );
  return done;
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
  dialog.addEventListener(
    "close",
    () => {
      dialog.remove();
      showNextMatch();
    },
    { once: true },
  );
  document.body.append(dialog);
  dialog.showModal();
}

// trackMatches notices matches created by other participants without interrupting swiping.
function trackMatches(state) {
  const matches = state.matches || [];
  const matchIDs = new Set(matches.map((movie) => movie.id));

  if (trackedRoomCode !== state.room.code || trackedRound !== state.room.round) {
    trackedRoomCode = state.room.code;
    trackedRound = state.room.round;
    knownMatchIDs = matchIDs;
    matchQueue = [];
    return;
  }

  const newMatches = matches.filter((movie) => !knownMatchIDs.has(movie.id));
  knownMatchIDs = matchIDs;
  if (newMatches.length === 1) {
    showToast(`New match: ${newMatches[0].title}`);
  } else if (newMatches.length > 1) {
    showToast(`${newMatches.length} new matches`);
  }
}

// queueMatch adds a locally completed match to the full-screen reveal queue.
function queueMatch(movie, markKnown) {
  if (markKnown) knownMatchIDs.add(movie.id);
  if (
    matchQueue.some((queued) => queued.id === movie.id) ||
    [...document.querySelectorAll(".match-dialog")].some(
      (dialog) => dialog.dataset.movieId === movie.id,
    )
  ) {
    return;
  }
  matchQueue.push(movie);
}

// resetMatchTracking clears match notification state when leaving a room.
function resetMatchTracking() {
  trackedRoomCode = "";
  trackedRound = 0;
  knownMatchIDs = new Set();
  matchQueue = [];
  for (const selector of [".match-dialog", ".matches-dialog"]) {
    const dialog = document.querySelector(selector);
    if (dialog?.open) dialog.close();
    else dialog?.remove();
  }
  matchDialogOpen = false;
}

// showNextMatch displays a prominent reveal only for the participant whose swipe completed it.
function showNextMatch() {
  if (
    matchDialogOpen ||
    matchQueue.length === 0 ||
    document.querySelector(".movie-dialog[open], .matches-dialog[open]")
  ) {
    return;
  }

  matchDialogOpen = true;
  const movie = matchQueue.shift();
  const dialog = el("dialog", "match-dialog");
  dialog.dataset.movieId = movie.id;
  const close = el("button", "dialog-close", "×");
  close.type = "button";
  close.setAttribute("aria-label", "Close match");
  close.onclick = () => dialog.close();

  const burst = el("div", "match-burst");
  ["♥", "♥", "♥", "♥", "♥", "♥"].forEach((heart, index) => {
    const particle = el("span", "match-heart", heart);
    particle.style.setProperty("--i", String(index));
    burst.append(particle);
  });

  const poster = el("img", "match-dialog-poster");
  poster.src = `/api/posters/${encodeURIComponent(movie.id)}`;
  poster.alt = `Poster for ${movie.title}`;

  const content = el("div", "match-dialog-content");
  content.append(
    el("p", "eyebrow match-eyebrow", "Everyone said yes"),
    el("h2", "", "It’s a match!"),
    el("p", "match-dialog-title", movie.title),
  );
  const metadata = movieMetadata(movie);
  if (metadata.length) content.append(el("p", "muted", metadata.join("  ·  ")));
  if (movie.genres?.length) {
    content.append(el("p", "match-dialog-genres", movie.genres.join(" · ")));
  }
  if (movie.summary) {
    content.append(el("p", "match-dialog-summary", movie.summary));
  }
  const continueButton = el(
    "button",
    "btn primary match-continue",
    "Continue swiping",
  );
  continueButton.type = "button";
  continueButton.onclick = () => dialog.close();
  content.append(continueButton);

  const layout = el("div", "match-dialog-layout");
  layout.append(poster, content);
  dialog.append(burst, close, layout);
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) dialog.close();
  });
  dialog.addEventListener(
    "close",
    () => {
      dialog.remove();
      matchDialogOpen = false;
      showNextMatch();
    },
    { once: true },
  );
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
function enableSwipe(card, movie) {
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
      vote(movie, delta > 0, card);
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
async function vote(movie, liked, card) {
  if (voting) return;
  const session = getSession();
  voting = true;
  card.style.transition = "transform .28s ease, opacity .28s";
  card.style.transform = `translateX(${liked ? 130 : -130}%) rotate(${liked ? 16 : -16}deg)`;
  card.style.opacity = 0;
  try {
    const result = await api(
      `/api/rooms/${encodeURIComponent(session.code)}/votes`,
      {
        method: "POST",
        body: JSON.stringify({ movieId: movie.id, liked }),
      },
    );
    if (result.matched) {
      queueMatch(movie, true);
      showNextMatch();
    }
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
    if ((event.data === "changed" || event.data === "connected") && !voting) {
      renderRoom();
    }
  });
  eventSource.onerror = () => {
    stopRoomEvents();
    setTimeout(connectEvents, 3000);
  };
}
