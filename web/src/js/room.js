import { api } from "./api.js";
import { qrcode } from "./vendor/qrcode.mjs";
import { getConfig, getSession, saveSession } from "./state.js";
import {
  confirmAction,
  instantiateTemplate,
  loadingElement,
  messageElement,
  root,
  showToast,
  templateElement,
  topbar,
} from "./ui.js";

const reconnectIndicatorDelay = 5000;
const reconnectWarningDelay = 30000;

let eventSource;
let voting = false;
let navigation;
let trackedRoomCode = "";
let trackedRound = 0;
let knownMatchIDs = new Set();
let matchQueue = [];
let matchDialogOpen = false;
let roomViewGeneration = 0;
let posterPreloads = new Map();
let roomConnectionState = "connecting";
let roomConnectionIndicatorVisible = false;
let reconnectIndicatorTimer;
let reconnectWarningTimer;

// renderRoom loads and displays the current room.
export async function renderRoom(nextNavigation) {
  if (nextNavigation) navigation = nextNavigation;
  const session = getSession();
  if (!session) return navigation.renderHome();
  const generation = roomViewGeneration;
  if (!root.querySelector(".room-head")) {
    root.replaceChildren(topbar(), loadingElement("Loading your room…"));
  }
  try {
    const state = await api(`/api/rooms/${encodeURIComponent(session.code)}`);
    if (generation !== roomViewGeneration) return;
    preloadRoomPosters(state);
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
  posterPreloads = new Map();
  clearReconnectTimers();
  roomConnectionState = "connecting";
  roomConnectionIndicatorVisible = false;
}

// preloadRoomPosters keeps the current and upcoming poster requests warm.
function preloadRoomPosters(state) {
  const itemIDs = [
    state.candidate?.id,
    ...(state.posterLookahead || []),
  ].filter(Boolean);
  const wanted = new Set(itemIDs);

  for (const itemID of posterPreloads.keys()) {
    if (!wanted.has(itemID)) posterPreloads.delete(itemID);
  }
  for (const itemID of itemIDs) {
    if (posterPreloads.has(itemID)) continue;
    const image = new Image();
    image.src = `/api/posters/${encodeURIComponent(itemID)}`;
    posterPreloads.set(itemID, image);
  }
}

// drawRoom fills the static room shell with live room state.
function drawRoom(state) {
  const session = getSession();
  const { fragment, refs } = instantiateTemplate("room-template");

  refs.roomEyebrow.textContent = [
    `Round ${state.room.round}`,
    phaseLabel(state.room.phase),
    state.room.locked ? "locked" : null,
  ]
    .filter(Boolean)
    .join(" · ");
  refs.roomHeading.textContent = `Good hunting, ${state.me.name}.`;
  renderParticipants(refs.participants, state);
  configureRoomCode(refs.roomCode, state.room.code);
  renderRoomMain(refs.roomContent, state);
  refs.progress.textContent = roomProgressText(state).join(" · ");

  refs.matchesHeading.textContent = `Round ${state.room.round} matches · ${(state.matches || []).length}`;
  refs.matchSummary.replaceChildren(matchSummary(state));

  const moreTitles = moreTitlesPanel(state);
  if (moreTitles) refs.roomControls.append(moreTitles);
  const nextRound = nextRoundPanel(state);
  if (nextRound) refs.roomControls.append(nextRound);
  const settings = roomSettingsPanel(state);
  if (settings) refs.roomControls.append(settings);

  root.replaceChildren(roomTopbar(session, state.room.code), fragment);
}

// roomSettingsPanel renders host controls for room lifetime and admission.
function roomSettingsPanel(state) {
  if (!state.me.isHost) return null;
  const { element: panel, refs } = templateElement(
    "room-settings-panel-template",
  );
  const expiresAt = new Date(state.room.expiresAt);
  refs.expiry.textContent = Number.isNaN(expiresAt.getTime())
    ? "Room expiry unavailable"
    : `Expires ${expiresAt.toLocaleString()}`;
  refs.status.textContent = state.room.locked
    ? "Locked · only existing participants can return."
    : "Open · anyone with the invite can join.";
  refs.toggle.textContent = state.room.locked ? "Unlock room" : "Lock room";
  refs.toggle.onclick = () => toggleRoomLock(state, refs.toggle);
  return panel;
}

// toggleRoomLock changes whether the room accepts new participants.
async function toggleRoomLock(state, button) {
  if (button.disabled) return;
  const session = getSession();
  const locked = !state.room.locked;
  button.disabled = true;
  button.textContent = locked ? "Locking…" : "Unlocking…";
  try {
    await api(`/api/rooms/${encodeURIComponent(session.code)}/settings`, {
      method: "PATCH",
      body: JSON.stringify({ locked }),
    });
    showToast(locked ? "Room locked" : "Room unlocked");
    await renderRoom();
  } catch (error) {
    showToast(error.message);
    button.disabled = false;
    button.textContent = state.room.locked ? "Unlock room" : "Lock room";
  }
}

// renderConnectionState displays connection progress only while live updates are unavailable.
function renderConnectionState(node) {
  node.hidden =
    roomConnectionState !== "reconnecting" || !roomConnectionIndicatorVisible;
}

// setRoomConnectionState updates the connection indicator in the active room.
function setRoomConnectionState(state) {
  if (state === roomConnectionState) return;
  roomConnectionState = state;
  if (state === "reconnecting") {
    scheduleReconnectFeedback();
  } else {
    clearReconnectTimers();
    roomConnectionIndicatorVisible = false;
  }
  const node = root.querySelector('[data-ref="connection"]');
  if (node) renderConnectionState(node);
}

// scheduleReconnectFeedback delays visible feedback for transient connection failures.
function scheduleReconnectFeedback() {
  reconnectIndicatorTimer = setTimeout(() => {
    reconnectIndicatorTimer = undefined;
    if (roomConnectionState !== "reconnecting") return;
    roomConnectionIndicatorVisible = true;
    const node = root.querySelector('[data-ref="connection"]');
    if (node) renderConnectionState(node);
  }, reconnectIndicatorDelay);
  reconnectWarningTimer = setTimeout(() => {
    reconnectWarningTimer = undefined;
    if (roomConnectionState === "reconnecting") {
      showToast("Live updates unavailable. Retrying…");
    }
  }, reconnectWarningDelay);
}

// clearReconnectTimers cancels pending connection feedback.
function clearReconnectTimers() {
  clearTimeout(reconnectIndicatorTimer);
  clearTimeout(reconnectWarningTimer);
  reconnectIndicatorTimer = undefined;
  reconnectWarningTimer = undefined;
}

// roomTopbar creates navigation actions for the active room.
function roomTopbar(session, roomCode) {
  const { element: actions, refs } = templateElement(
    "room-topbar-actions-template",
  );
  refs.rooms.onclick = () => {
    stopRoomEvents();
    resetMatchTracking();
    saveSession(null);
    navigation.renderHome();
  };
  renderConnectionState(refs.connection);
  refs.invite.onclick = () => showInviteDialog(roomCode);
  refs.leave.onclick = () => leaveCurrentRoom(refs.leave, session);
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

// renderParticipants fills the room participant list and host removal controls.
function renderParticipants(container, state) {
  const canRemoveParticipants =
    state.me.isHost && state.participants.length > 1;

  state.participants.forEach((participant) => {
    const labels = [participant.name];
    if (participant.id === state.me.id) labels.push("you");
    if (participant.isHost) labels.push("host");
    if (participant.readyForNextRound) labels.push("next round ✓");

    const { element: person, refs } = templateElement("participant-template");
    if (participant.id === state.me.id) person.classList.add("me");
    if (participant.readyForNextRound) person.classList.add("ready");
    person.title = participant.genres?.length
      ? `Genres (${participant.genreMode === "all" ? "all" : "any"}): ${participant.genres.join(", ")}`
      : "Genres: everything";
    refs.label.textContent = labels.join(" · ");

    if (canRemoveParticipants && participant.id !== state.me.id) {
      refs.remove.hidden = false;
      refs.remove.title = `Remove ${participant.name}`;
      refs.remove.setAttribute(
        "aria-label",
        `Remove ${participant.name} from the room`,
      );
      refs.remove.onclick = (event) => {
        event.stopPropagation();
        removeParticipant(participant, refs.remove);
      };
    }
    container.append(person);
  });
}

// configureRoomCode wires the static room-code button to the share action.
function configureRoomCode(button, roomCode) {
  button.textContent = roomCode;
  button.onclick = () => showInviteDialog(roomCode);
}

// showInviteDialog presents the room code and every supported sharing action.
function showInviteDialog(roomCode) {
  document.querySelector(".invite-dialog")?.remove();
  const inviteURL = roomURL(roomCode);
  const { element: dialog, refs } = templateElement("invite-dialog-template");
  refs.close.onclick = () => dialog.close();
  refs.code.textContent = roomCode;
  refs.link.value = inviteURL;
  refs.qr.setAttribute("aria-label", `QR code to join room ${roomCode}`);
  try {
    renderInviteQRCode(refs.qr, inviteURL);
  } catch {
    refs.qrWrap.hidden = true;
  }
  refs.link.onclick = () => refs.link.select();
  refs.copyCode.onclick = () => copyInviteValue(roomCode, "Room code copied");
  refs.copyLink.onclick = () =>
    copyInviteValue(inviteURL, "Invite link copied");

  if (typeof navigator.share === "function") {
    refs.share.hidden = false;
    refs.share.onclick = async () => {
      try {
        await navigator.share({
          title: `Join ScreenDeck room ${roomCode}`,
          text: `Join my ScreenDeck room with code ${roomCode}.`,
          url: inviteURL,
        });
      } catch (error) {
        if (error.name !== "AbortError") showToast("Could not open sharing");
      }
    };
  }

  showModalDialog(dialog);
  refs.copyLink.focus();
}

// renderInviteQRCode draws a locally generated invite QR code with a scanner-safe border.
function renderInviteQRCode(canvas, value) {
  const code = qrcode(0, "M");
  code.addData(value);
  code.make();

  const quietZone = 4;
  const moduleCount = code.getModuleCount();
  const totalModules = moduleCount + quietZone * 2;
  const scale = Math.max(1, Math.floor(240 / totalModules));
  const size = totalModules * scale;
  const context = canvas.getContext("2d");
  canvas.width = size;
  canvas.height = size;
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, size, size);
  context.fillStyle = "#111014";

  for (let row = 0; row < moduleCount; row += 1) {
    for (let column = 0; column < moduleCount; column += 1) {
      if (!code.isDark(row, column)) continue;
      context.fillRect(
        (column + quietZone) * scale,
        (row + quietZone) * scale,
        scale,
        scale,
      );
    }
  }
}

// copyInviteValue writes an invite value to the clipboard and reports the result.
async function copyInviteValue(value, successMessage) {
  try {
    await navigator.clipboard.writeText(value);
    showToast(successMessage);
  } catch {
    showToast("Could not copy to the clipboard");
  }
}

// renderRoomMain fills the active-card area with the current room state.
function renderRoomMain(container, state) {
  if (state.room.phase === "finished" && state.winner) {
    container.append(winnerCard(state));
  } else if (state.candidate) {
    appendSwipeCandidate(container, state.candidate);
  } else {
    container.append(finishedCard(state));
  }
}

// appendSwipeCandidate adds the active card and swipe controls to the room view.
function appendSwipeCandidate(container, item) {
  const showDetails = () => showItemDetails(item);
  const { fragment, refs } = instantiateTemplate("swipe-view-template");
  const card = itemCard(item, showDetails);
  refs.deck.append(card);
  refs.no.onclick = () => vote(item, false, card);
  refs.details.onclick = showDetails;
  refs.yes.onclick = () => vote(item, true, card);
  container.append(fragment);
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
    return messageElement(
      "empty-template",
      state.participants.length < 2
        ? "Invite someone with the room code. Matches need at least two people."
        : "A shared yes will appear here.",
    );
  }

  const { element: pile, refs } = templateElement("match-pile-template");
  pile.setAttribute(
    "aria-label",
    `Show ${matches.length} ${matches.length === 1 ? "match" : "matches"}`,
  );
  pile.onclick = () => showMatches(matches, state.room.round);

  matches.slice(0, 3).forEach((item) => {
    const { element: image, refs: imageRefs } = templateElement(
      "match-pile-poster-template",
    );
    setPoster(imageRefs.poster, item, true);
    refs.count.before(image);
  });
  refs.count.textContent = String(matches.length);
  refs.label.textContent = `${matches.length} ${matches.length === 1 ? "match" : "matches"}`;
  return pile;
}

// showMatches expands the current match pile in a scrollable dialog.
function showMatches(matches, round) {
  document.querySelector(".matches-dialog")?.remove();
  const { element: dialog, refs } = templateElement("matches-dialog-template");
  refs.close.onclick = () => dialog.close();
  refs.round.textContent = `Round ${round}`;
  refs.heading.textContent = `${matches.length} ${matches.length === 1 ? "match" : "matches"}`;

  matches.forEach((item) => {
    const { element: button, refs: itemRefs } = templateElement(
      "match-grid-item-template",
    );
    button.title = `View details for ${item.title}`;
    setPoster(itemRefs.poster, item);
    itemRefs.title.textContent = item.title;
    button.onclick = () => {
      dialog.close();
      showItemDetails(item);
    };
    refs.list.append(button);
  });

  showModalDialog(dialog, showNextMatch);
}

// moreTitlesPanel lets the room expand the first round from its unused reserve.
function moreTitlesPanel(state) {
  const available = state.moreTitles?.available || 0;
  if (state.room.round !== 1 || available <= 0) return null;

  const { element: panel, refs } = templateElement(
    "more-titles-panel-template",
  );
  refs.description.textContent = `${available} unused titles remain from the original filtered pool.`;
  if (!state.me.isHost) {
    refs.hostOnly.hidden = false;
    refs.actions.remove();
    return panel;
  }

  [50, 100, 250].forEach((count) => {
    const amount = Math.min(count, available);
    if (amount <= 0) return;
    const { element: button } = templateElement("more-titles-button-template");
    button.textContent = `+${amount}`;
    button.onclick = () => addMoreTitles(amount, button);
    refs.actions.append(button);
  });
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

  const { element: panel, refs } = templateElement("next-round-panel-template");
  configureNextRoundStatus(refs, state, readiness);

  if (matches.length < 2 && state.me.readyForNextRound) {
    refs.shortage.hidden = false;
  }

  configureNextRoundAction(refs.action, state, matches, readiness);
  return panel;
}

// configureNextRoundStatus fills the current request state and participant readiness.
function configureNextRoundStatus(refs, state, readiness) {
  if (readiness.ready === 0) {
    refs.initialMessage.hidden = false;
    return;
  }

  const requester = readiness.requestedBy;
  refs.requestStatus.hidden = false;
  refs.requestStatus.textContent = requester
    ? `${requester.id === state.me.id ? "You" : requester.name} asked for the next round.`
    : "A next round was requested.";
  refs.readiness.hidden = false;
  refs.readiness.textContent = `${readiness.ready} of ${readiness.required} ready`;
  refs.roster.hidden = false;

  state.participants.forEach((participant) => {
    const ready = participant.readyForNextRound;
    const { element: person, refs: personRefs } = templateElement(
      "next-round-person-template",
    );
    if (ready) person.classList.add("ready");
    personRefs.person.textContent = `${ready ? "✓" : "○"} ${participant.name}${participant.id === state.me.id ? " · you" : ""}`;
    refs.roster.append(person);
  });
}

// configureNextRoundAction configures this participant's readiness action when available.
function configureNextRoundAction(button, state, matches, readiness) {
  if (matches.length < 2 && !state.me.readyForNextRound) return;

  button.hidden = false;
  button.className = state.me.readyForNextRound
    ? "btn ghost next-round-button"
    : "btn primary next-round-button";
  button.textContent = state.me.readyForNextRound
    ? "Withdraw readiness"
    : readiness.ready > 0
      ? "Ready for next round"
      : "Ask for next round";
  button.onclick = () => toggleNextRoundReady(state, button);
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
  const { element: card, refs } = templateElement("winner-card-template");
  setPoster(refs.poster, item);
  refs.title.textContent = item.title;
  refs.meta.textContent = itemMetadata(item);

  if (item.genres?.length) {
    refs.genres.hidden = false;
    refs.genres.textContent = item.genres.join(" · ");
  }
  if (item.summary) {
    refs.summary.hidden = false;
    refs.summary.textContent = item.summary;
  }

  const supporters = (winner.likedBy || []).map(
    (participant) => participant.name,
  );
  refs.likedBy.textContent = supporters.length
    ? `Liked by ${supporters.join(", ")}`
    : "The final shared match.";
  refs.details.onclick = () => showItemDetails(item);
  refs.restart.onclick = startNewRoom;
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
  const { element: done, refs } = templateElement("finished-card-template");
  const matches = state.matches || [];

  if (state.progress.total === 0 && state.progress.filteredOut > 0) {
    refs.eyebrow.textContent = "Personal deck";
    refs.heading.textContent = "No titles match your genres.";
    refs.message.textContent = `${state.progress.filteredOut} round titles were excluded by your ${state.me.genreMode === "all" ? "match-all" : "match-any"} genre preference.`;
    return done;
  }

  if (state.roundComplete && matches.length === 1) {
    done.classList.add("winner");
    refs.eyebrow.textContent = "Decision made";
    refs.heading.textContent = "One title remains.";
    refs.message.textContent = `${matches[0].title} survived the complete round.`;
    return done;
  }

  if (state.roundComplete && matches.length === 0) {
    refs.eyebrow.textContent = "Round complete";
    refs.heading.textContent = "No shared pick survived.";
    refs.message.textContent =
      "Start a new room with a wider deck or different filters to try again.";
    return done;
  }

  if (matches.length > 1) {
    refs.eyebrow.textContent = "Your deck is complete";
    refs.heading.textContent = `${matches.length} matches so far.`;
    refs.message.textContent =
      "You can wait for more matches or use the next-round request to narrow the group now.";
    return done;
  }

  refs.eyebrow.textContent = "You’re done for now";
  refs.heading.textContent = "You’ve seen your whole deck.";
  refs.message.textContent = state.roundComplete
    ? "The group has finished this deck."
    : "Other participants can keep swiping; new matches will appear live.";
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
  const { element: card, refs } = templateElement("item-card-template");
  card.setAttribute("aria-label", `View details for ${item.title}`);
  card.title = "View details";
  card.onclick = showDetails;
  card.onkeydown = (event) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    showDetails();
  };
  setPoster(refs.poster, item);
  refs.title.textContent = item.title;
  refs.meta.textContent = [
    item.type === "show" ? "TV series" : "Movie",
    item.year || null,
    item.type === "movie" && item.duration
      ? `${Math.round(item.duration / 60000)} min`
      : null,
    item.rating ? `★ ${item.rating.toFixed(1)}` : null,
  ]
    .filter(Boolean)
    .join("  ·  ");
  refs.summary.textContent =
    item.summary || item.genres?.join(" · ") || "No synopsis available.";
  return card;
}

// showItemDetails opens the complete metadata and synopsis for a title.
function showItemDetails(item) {
  document.querySelector(".item-dialog")?.remove();
  const { element: dialog, refs } = templateElement("item-dialog-template");
  refs.close.onclick = () => dialog.close();
  setPoster(refs.poster, item);
  refs.type.textContent = item.type === "show" ? "TV series" : "Movie";
  refs.title.textContent = item.title;
  refs.meta.textContent = itemMetadata(item).join("  ·  ");
  refs.summary.textContent = item.summary || "No synopsis available.";
  if (item.genres?.length) {
    refs.genres.hidden = false;
    refs.genres.textContent = item.genres.join(" · ");
  }
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
  const { element: dialog, refs } = templateElement("match-dialog-template");
  dialog.dataset.itemId = item.id;
  refs.close.onclick = () => dialog.close();
  setPoster(refs.poster, item);
  refs.title.textContent = item.title;

  const metadata = itemMetadata(item);
  if (metadata.length) {
    refs.meta.hidden = false;
    refs.meta.textContent = metadata.join("  ·  ");
  }
  if (item.genres?.length) {
    refs.genres.hidden = false;
    refs.genres.textContent = item.genres.join(" · ");
  }
  if (item.summary) {
    refs.summary.hidden = false;
    refs.summary.textContent = item.summary;
  }
  refs.continue.onclick = () => dialog.close();

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
  if (roomConnectionState !== "reconnecting") {
    setRoomConnectionState("connecting");
  }
  source.onopen = () => {
    if (generation !== roomViewGeneration || source !== eventSource) return;
    setRoomConnectionState("connected");
  };
  source.addEventListener("update", (event) => {
    if (generation !== roomViewGeneration || source !== eventSource) return;
    if ((event.data === "changed" || event.data === "connected") && !voting) {
      renderRoom();
    }
  });
  source.onerror = () => {
    if (generation !== roomViewGeneration || source !== eventSource) return;
    console.debug("Room live updates interrupted; reconnecting.");
    source.close();
    eventSource = null;
    setRoomConnectionState("reconnecting");
    setTimeout(() => {
      if (generation === roomViewGeneration) connectEvents();
    }, 3000);
  };
}

// setPoster loads an item poster and substitutes the ScreenDeck mark after failures.
function setPoster(image, item, decorative = false) {
  image.onerror = () => {
    image.onerror = null;
    image.classList.add("poster-fallback");
    image.src = "/favicon.svg";
    if (!decorative) image.alt = `Poster unavailable for ${item.title}`;
  };
  image.src = `/api/posters/${encodeURIComponent(item.id)}`;
  image.alt = decorative ? "" : `Poster for ${item.title}`;
}
