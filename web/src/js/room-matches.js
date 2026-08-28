import { itemMetadata, setPoster, showItemDetails } from "./room-media.js";
import {
  messageElement,
  showModalDialog,
  showToast,
  templateElement,
} from "./ui.js";

let trackedRoomCode = "";
let trackedRound = 0;
let knownMatchIDs = new Set();
let matchQueue = [];
let matchDialogOpen = false;

// matchSummary renders a compact stack instead of every matched poster.
export function matchSummary(state) {
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
      showItemDetails(item, undefined, showNextMatch);
    };
    refs.list.append(button);
  });

  showModalDialog(dialog, showNextMatch);
}

// trackMatches notices matches created by other participants without interrupting swiping.
export function trackMatches(state) {
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
export function queueMatch(item, markKnown) {
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
export function resetMatchTracking() {
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
export function showNextMatch() {
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
