import { api } from "./api.js";
import { getConfig, getSession, saveSession } from "./state.js";
import { confirmAction, el, root, showToast, topbar } from "./ui.js";

let eventSource;
let voting = false;
let navigation;
let trackedRoomCode = "";
let trackedRound = 0;
let knownMatchIDs = new Set();
let matchQueue = [];
let matchDialogOpen = false;
let roomViewGeneration = 0;

// renderRoom loads and displays the current room.
export async function renderRoom(nextNavigation) {
  if (nextNavigation) navigation = nextNavigation;
  const session = getSession();
  if (!session) return navigation.renderHome();
  const generation = roomViewGeneration;
  try {
    const state = await api(`/api/rooms/${encodeURIComponent(session.code)}`);
    if (generation !== roomViewGeneration) return;
    drawRoom(state);
    trackMatches(state);
    connectEvents();
  } catch (error) {
    if (generation !== roomViewGeneration) return;
    resetMatchTracking();
    saveSession(null);
    navigation.renderHome();
    showToast(error.message);
  }
}

// stopRoomEvents closes the active room event stream.
export function stopRoomEvents() {
  roomViewGeneration += 1;
  eventSource?.close();
  eventSource = null;
}

// drawRoom renders participants, the current candidate, matches, and next-round readiness.
function drawRoom(state) {
  const session = getSession();
  root.replaceChildren();
  root.append(roomTopbar(session), roomHeader(state));

  const grid = el("section", "room-grid");
  grid.append(roomMain(state), roomSidebar(state));
  root.append(grid);
}

// roomTopbar creates navigation actions for the active room.
function roomTopbar(session) {
  const rooms = el("button", "btn ghost", "My rooms");
  rooms.onclick = () => {
    stopRoomEvents();
    resetMatchTracking();
    saveSession(null);
    navigation.renderHome();
  };

  const leave = el("button", "btn ghost", "Leave");
  leave.onclick = () => leaveCurrentRoom(leave, session);

  const actions = el("div", "topbar-actions");
  actions.append(rooms, leave);
  return topbar(actions);
}

// leaveCurrentRoom confirms departure and clears the local participant session.
async function leaveCurrentRoom(button, session) {
  if (button.disabled) return;
  button.disabled = true;

  const confirmed = await confirmAction({
    title: "Leave room?",
    message:
      "Leaving removes your membership from this room. Your votes will no longer count toward matches.",
    confirmLabel: "Leave room",
    destructive: true,
  });
  if (!confirmed) {
    button.disabled = false;
    return;
  }

  stopRoomEvents();
  try {
    await api(`/api/rooms/${encodeURIComponent(session.code)}`, {
      method: "DELETE",
    });
  } catch {
    /* session may already be gone */
  }

  resetMatchTracking();
  saveSession(null);
  navigation.renderHome();
}

// roomHeader renders the round summary, participants, and share code.
function roomHeader(state) {
  const head = el("section", "room-head");
  const intro = el("div");
  intro.append(
    el(
      "div",
      "eyebrow",
      `Round ${state.room.round} · ${phaseLabel(state.room.phase)}`,
    ),
    el("h2", "", `Good hunting, ${state.me.name}.`),
    participantList(state),
  );

  head.append(intro, roomCodeButton(state.room.code));
  return head;
}

// participantList renders room participants and host removal controls.
function participantList(state) {
  const people = el("div", "people");
  const canRemoveParticipants =
    state.me.isHost && state.participants.length > 1;

  state.participants.forEach((participant) => {
    const labels = [participant.name];
    if (participant.id === state.me.id) labels.push("you");
    if (participant.isHost) labels.push("host");
    if (participant.readyForNextRound) labels.push("next round ✓");

    const person = el(
      "div",
      `person${participant.id === state.me.id ? " me" : ""}${participant.readyForNextRound ? " ready" : ""}`,
    );
    person.title = participant.genres?.length
      ? `Genres (${participant.genreMode === "all" ? "all" : "any"}): ${participant.genres.join(", ")}`
      : "Genres: everything";
    person.append(el("span", "person-label", labels.join(" · ")));

    if (canRemoveParticipants && participant.id !== state.me.id) {
      const remove = el("button", "person-remove", "×");
      remove.type = "button";
      remove.title = `Remove ${participant.name}`;
      remove.setAttribute(
        "aria-label",
        `Remove ${participant.name} from the room`,
      );
      remove.onclick = (event) => {
        event.stopPropagation();
        removeParticipant(participant, remove);
      };
      person.append(remove);
    }
    people.append(person);
  });

  return people;
}

// roomCodeButton creates the share control for the current room.
function roomCodeButton(roomCode) {
  const code = el("button", "room-code", roomCode);
  code.title = "Copy room link";
  code.onclick = async () => {
    try {
      await navigator.clipboard.writeText(roomURL(roomCode));
      showToast("Room link copied");
    } catch {
      showToast("Could not copy room link");
    }
  };
  return code;
}

// roomMain renders the active card, terminal room state, and personal progress.
function roomMain(state) {
  const main = el("div");
  if (state.room.phase === "finished" && state.winner) {
    main.append(winnerCard(state));
  } else if (state.candidate) {
    appendSwipeCandidate(main, state.candidate);
  } else {
    main.append(finishedCard(state));
  }

  main.append(el("div", "progress", roomProgressText(state).join(" · ")));
  return main;
}

// appendSwipeCandidate adds the active card and swipe controls to the room view.
function appendSwipeCandidate(container, item) {
  const showDetails = () => showItemDetails(item);
  const deck = el("div", "deck");
  const card = itemCard(item, showDetails);
  deck.append(card);
  container.append(deck);

  const actions = el("div", "swipe-actions");
  const no = el("button", "btn icon no", "×");
  no.title = "Pass";
  no.onclick = () => vote(item, false, card);

  const details = el("button", "btn icon info", "i");
  details.title = "View details";
  details.setAttribute("aria-label", "View title details");
  details.onclick = showDetails;

  const yes = el("button", "btn icon yes", "♥");
  yes.title = "Like";
  yes.onclick = () => vote(item, true, card);
  actions.append(no, details, yes);
  container.append(actions);
  enableSwipe(card, item);
}

// roomProgressText returns the personal progress labels shown below the deck.
function roomProgressText(state) {
  const labels = [
    `${state.progress.voted} of ${state.progress.total} considered`,
    `round ${state.room.round}`,
  ];
  if (state.progress.filteredOut > 0) {
    labels.push(
      `${state.progress.filteredOut} of ${state.progress.roundTotal} excluded by your genre preferences`,
    );
  }
  return labels;
}

// roomSidebar renders matches and room-wide round controls.
function roomSidebar(state) {
  const side = el("aside", "side");
  side.append(
    el(
      "h3",
      "",
      `Round ${state.room.round} matches · ${(state.matches || []).length}`,
    ),
    matchSummary(state),
  );

  const moreTitles = moreTitlesPanel(state);
  if (moreTitles) side.append(moreTitles);
  const nextRound = nextRoundPanel(state);
  if (nextRound) side.append(nextRound);
  return side;
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
  matches.slice(0, 3).forEach((item) => {
    const image = el("img", "match-pile-poster");
    image.src = `/api/posters/${encodeURIComponent(item.id)}`;
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
    el(
      "h2",
      "",
      `${matches.length} ${matches.length === 1 ? "match" : "matches"}`,
    ),
    el("p", "muted", "These are the titles everyone has liked so far."),
  );

  const list = el("div", "matches-dialog-grid");
  matches.forEach((item) => {
    const button = el("button", "match-grid-item");
    button.type = "button";
    button.title = `View details for ${item.title}`;
    const image = el("img");
    image.src = `/api/posters/${encodeURIComponent(item.id)}`;
    image.alt = `Poster for ${item.title}`;
    button.append(image, el("span", "", item.title));
    button.onclick = () => {
      dialog.close();
      showItemDetails(item);
    };
    list.append(button);
  });

  dialog.append(close, header, list);
  showModalDialog(dialog, showNextMatch);
}

// moreTitlesPanel lets the room expand the first round from its unused reserve.
function moreTitlesPanel(state) {
  const available = state.moreTitles?.available || 0;
  if (state.room.round !== 1 || available <= 0) return null;

  const panel = el("section", "more-titles-panel");
  panel.append(
    el("h3", "", "Need more options?"),
    el(
      "p",
      "muted",
      `${available} unused titles remain from the original filtered pool.`,
    ),
  );
  if (!state.me.isHost) {
    panel.append(el("p", "muted", "The room host can add more titles."));
    return panel;
  }
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
  appendNextRoundStatus(panel, state, readiness);

  if (matches.length < 2 && state.me.readyForNextRound) {
    panel.append(
      el(
        "p",
        "muted",
        "There are fewer than two matches now. Withdraw your readiness or keep swiping until another match appears.",
      ),
    );
  }

  appendNextRoundAction(panel, state, matches, readiness);
  return panel;
}

// appendNextRoundStatus adds the current request state and participant readiness.
function appendNextRoundStatus(panel, state, readiness) {
  if (readiness.ready === 0) {
    panel.append(
      el(
        "p",
        "muted",
        "Ask everyone to narrow the deck to the matches you have right now. Readiness resets if the group changes or fewer than two matches remain.",
      ),
    );
    return;
  }

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
}

// appendNextRoundAction adds the current participant's readiness action when available.
function appendNextRoundAction(panel, state, matches, readiness) {
  if (matches.length < 2 && !state.me.readyForNextRound) return;

  const button = el(
    "button",
    state.me.readyForNextRound
      ? "btn ghost next-round-button"
      : "btn primary next-round-button",
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

// winnerCard renders the final shared choice as a dedicated result screen.
function winnerCard(state) {
  const winner = state.winner;
  const item = winner.item;
  const card = el("section", "winner-card");
  const poster = el("img", "winner-poster");
  poster.src = `/api/posters/${encodeURIComponent(item.id)}`;
  poster.alt = `Poster for ${item.title}`;

  const content = el("div", "winner-content");
  content.append(
    el("div", "eyebrow", "Tonight, decided."),
    el("h2", "", item.title),
    el("p", "winner-meta", itemMetadata(item)),
  );
  if (item.genres?.length) {
    content.append(el("p", "dialog-genres", item.genres.join(" · ")));
  }
  if (item.summary) {
    content.append(el("p", "winner-summary", item.summary));
  }
  const supporters = (winner.likedBy || []).map(
    (participant) => participant.name,
  );
  content.append(
    el(
      "p",
      "winner-liked-by",
      supporters.length
        ? `Liked by ${supporters.join(", ")}`
        : "The final shared match.",
    ),
  );

  const actions = el("div", "winner-actions");
  const details = el("button", "btn ghost", "View details");
  details.type = "button";
  details.onclick = () => showItemDetails(item);
  const restart = el("button", "btn primary", "Start new room");
  restart.type = "button";
  restart.onclick = startNewRoom;
  actions.append(details, restart);
  content.append(actions);
  card.append(poster, content);
  return card;
}

// startNewRoom keeps the current membership and opens a fresh host flow.
async function startNewRoom() {
  stopRoomEvents();
  resetMatchTracking();
  saveSession(null);
  if (navigation.renderCreateRoom) navigation.renderCreateRoom();
  else navigation.renderHome();
}

// finishedCard renders the state shown after this participant exhausts their personal deck.
function finishedCard(state) {
  const done = el("div", "finished");
  const matches = state.matches || [];

  if (state.progress.total === 0 && state.progress.filteredOut > 0) {
    done.append(
      el("div", "eyebrow", "Personal deck"),
      el("h2", "", "No titles match your genres."),
      el(
        "p",
        "lede",
        `${state.progress.filteredOut} round titles were excluded by your ${state.me.genreMode === "all" ? "match-all" : "match-any"} genre preference.`,
      ),
    );
    return done;
  }

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

// removeParticipant lets the room host kick another participant out of the room.
async function removeParticipant(participant, button) {
  if (button.disabled) return;
  button.disabled = true;
  const confirmed = await confirmAction({
    title: "Remove participant?",
    message: `Remove ${participant.name} from this room? Their votes and round readiness will no longer count.`,
    confirmLabel: "Remove",
    destructive: true,
  });
  if (!confirmed) {
    button.disabled = false;
    return;
  }

  const session = getSession();
  try {
    await api(
      `/api/rooms/${encodeURIComponent(session.code)}/participants/${encodeURIComponent(participant.id)}`,
      { method: "DELETE" },
    );
    showToast(`${participant.name} was removed`);
    await renderRoom();
  } catch (error) {
    showToast(error.message);
    button.disabled = false;
  }
}

// roomURL builds a direct join URL for a room code.
function roomURL(roomCode) {
  const url = new URL(getConfig().baseUrl || window.location.origin);
  url.search = "";
  url.hash = "";
  url.searchParams.set("room", roomCode);
  return url.toString();
}

// itemCard builds a swipeable card for one media item.
function itemCard(item, showDetails) {
  const card = el("article", "card");
  card.tabIndex = 0;
  card.setAttribute("role", "button");
  card.setAttribute("aria-label", `View details for ${item.title}`);
  card.title = "View details";
  card.onclick = showDetails;
  card.onkeydown = (event) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    showDetails();
  };
  const image = el("img", "poster");
  image.src = `/api/posters/${encodeURIComponent(item.id)}`;
  image.alt = `Poster for ${item.title}`;
  const nope = el("div", "stamp nope", "NOPE");
  const like = el("div", "stamp like", "LIKE");
  const body = el("div", "card-body");
  body.append(el("h2", "item-title", item.title));
  const meta = el("div", "meta");
  const bits = [
    item.type === "show" ? "TV series" : "Movie",
    item.year || null,
    item.type === "movie" && item.duration
      ? `${Math.round(item.duration / 60000)} min`
      : null,
    item.rating ? `★ ${item.rating.toFixed(1)}` : null,
  ].filter(Boolean);
  meta.textContent = bits.join("  ·  ");
  body.append(
    meta,
    el(
      "p",
      "summary",
      item.summary || item.genres?.join(" · ") || "No synopsis available.",
    ),
  );
  card.append(image, nope, like, body);
  return card;
}

// showItemDetails opens the complete metadata and synopsis for a title.
function showItemDetails(item) {
  document.querySelector(".item-dialog")?.remove();
  const dialog = el("dialog", "item-dialog");
  const close = el("button", "dialog-close", "×");
  close.type = "button";
  close.setAttribute("aria-label", "Close details");
  close.onclick = () => dialog.close();
  const image = el("img", "dialog-poster");
  image.src = `/api/posters/${encodeURIComponent(item.id)}`;
  image.alt = `Poster for ${item.title}`;
  const content = el("div", "dialog-content");
  content.append(
    el("div", "eyebrow", item.type === "show" ? "TV series" : "Movie"),
    el("h2", "", item.title),
    el("div", "meta", itemMetadata(item).join("  ·  ")),
    el("p", "dialog-summary", item.summary || "No synopsis available."),
  );
  if (item.genres?.length) {
    content.append(el("p", "dialog-genres", item.genres.join(" · ")));
  }
  dialog.append(close, image, content);
  showModalDialog(dialog, showNextMatch);
}

// trackMatches notices matches created by other participants without interrupting swiping.
function trackMatches(state) {
  const matches = state.matches || [];
  const matchIDs = new Set(matches.map((item) => item.id));

  if (
    trackedRoomCode !== state.room.code ||
    trackedRound !== state.room.round
  ) {
    trackedRoomCode = state.room.code;
    trackedRound = state.room.round;
    knownMatchIDs = matchIDs;
    matchQueue = [];
    return;
  }

  const newMatches = matches.filter((item) => !knownMatchIDs.has(item.id));
  knownMatchIDs = matchIDs;
  if (newMatches.length === 1) {
    showToast(`New match: ${newMatches[0].title}`);
  } else if (newMatches.length > 1) {
    showToast(`${newMatches.length} new matches`);
  }
}

// queueMatch adds a locally completed match to the full-screen reveal queue.
function queueMatch(item, markKnown) {
  if (markKnown) knownMatchIDs.add(item.id);
  if (
    matchQueue.some((queued) => queued.id === item.id) ||
    [...document.querySelectorAll(".match-dialog")].some(
      (dialog) => dialog.dataset.itemId === item.id,
    )
  ) {
    return;
  }
  matchQueue.push(item);
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
    document.querySelector(".item-dialog[open], .matches-dialog[open]")
  ) {
    return;
  }

  matchDialogOpen = true;
  const item = matchQueue.shift();
  const dialog = el("dialog", "match-dialog");
  dialog.dataset.itemId = item.id;
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
  poster.src = `/api/posters/${encodeURIComponent(item.id)}`;
  poster.alt = `Poster for ${item.title}`;

  const content = el("div", "match-dialog-content");
  content.append(
    el("p", "eyebrow match-eyebrow", "Everyone said yes"),
    el("h2", "", "It’s a match!"),
    el("p", "match-dialog-title", item.title),
  );
  const metadata = itemMetadata(item);
  if (metadata.length) content.append(el("p", "muted", metadata.join("  ·  ")));
  if (item.genres?.length) {
    content.append(el("p", "match-dialog-genres", item.genres.join(" · ")));
  }
  if (item.summary) {
    content.append(el("p", "match-dialog-summary", item.summary));
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
  showModalDialog(dialog, () => {
    matchDialogOpen = false;
    showNextMatch();
  });
}

// showModalDialog opens a dialog and removes it after backdrop or explicit closure.
function showModalDialog(dialog, onClose) {
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

// itemMetadata returns display-ready metadata for a media item.
function itemMetadata(item) {
  return [
    item.year || null,
    item.type === "movie" && item.duration
      ? `${Math.round(item.duration / 60000)} min`
      : null,
    item.rating ? `★ ${item.rating.toFixed(1)}` : null,
  ].filter(Boolean);
}

// enableSwipe adds pointer-driven voting gestures to a card.
function enableSwipe(card, item) {
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
      vote(item, delta > 0, card);
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
async function vote(item, liked, card) {
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
        body: JSON.stringify({ itemId: item.id, liked }),
      },
    );
    if (result.matched) {
      queueMatch(item, true);
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

  const generation = roomViewGeneration;
  const source = new EventSource(
    `/api/rooms/${encodeURIComponent(session.code)}/events?token=${encodeURIComponent(session.token)}`,
  );
  eventSource = source;
  source.addEventListener("update", (event) => {
    if (generation !== roomViewGeneration || source !== eventSource) return;
    if ((event.data === "changed" || event.data === "connected") && !voting) {
      renderRoom();
    }
  });
  source.onerror = () => {
    if (generation !== roomViewGeneration || source !== eventSource) return;
    stopRoomEvents();
    setTimeout(connectEvents, 3000);
  };
}

