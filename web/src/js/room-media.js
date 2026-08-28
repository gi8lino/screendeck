import { showModalDialog, templateElement } from "./ui.js";

let posterPreloads = new Map();

// resetPosterPreloads releases references to preloaded room posters.
export function resetPosterPreloads() {
  posterPreloads = new Map();
}

// preloadRoomPosters keeps the current and upcoming poster requests warm.
export function preloadRoomPosters(state) {
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

// itemCard builds a swipeable card for one media item.
export function itemCard(item, showDetails) {
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
export function showItemDetails(
  item,
  onVote,
  onClose,
  canVote = () => true,
) {
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
    refs.genres.replaceChildren(
      ...item.genres.map((genre) => {
        const chip = document.createElement("span");
        chip.textContent = genre;
        return chip;
      }),
    );
  }
  if (onVote) {
    dialog.classList.add("votable");
    refs.actions.hidden = false;
    const submitVote = (liked) => {
      if (!canVote()) return;
      dialog.close();
      onVote(liked);
    };
    refs.no.onclick = () => submitVote(false);
    refs.yes.onclick = () => submitVote(true);
    enableDetailsSwipe(dialog, submitVote);
  }
  showModalDialog(dialog, onClose);
}

// itemMetadata returns display-ready metadata for a media item.
export function itemMetadata(item) {
  return [
    item.year || null,
    item.type === "movie" && item.duration
      ? `${Math.round(item.duration / 60000)} min`
      : null,
    item.rating ? `★ ${item.rating.toFixed(1)}` : null,
  ].filter(Boolean);
}

// enableSwipe adds pointer-driven voting gestures to a card.
export function enableSwipe(card, onSwipe) {
  enableHorizontalSwipe(card, {
    onMove: (delta) => {
      card.style.transition = "none";
      card.style.transform = `translateX(${delta}px) rotate(${delta / 18}deg)`;
      card.querySelector(delta > 0 ? ".like" : ".nope").style.opacity =
        Math.min(Math.abs(delta) / 100, 1);
    },
    onReset: () => {
      card.style.transition = "";
      card.style.transform = "";
      card
        .querySelectorAll(".stamp")
        .forEach((stamp) => (stamp.style.opacity = 0));
    },
    onSwipe,
    suppressClick: true,
  });
}

// enableDetailsSwipe lets the active title be voted on without closing its details first.
function enableDetailsSwipe(dialog, onSwipe) {
  enableHorizontalSwipe(dialog.querySelector(".item-dialog-layout"), {
    onMove: (delta) => {
      dialog.style.transition = "none";
      dialog.style.transform = `translateX(${delta}px) rotate(${delta / 30}deg)`;
      dialog.querySelector(delta > 0 ? ".like" : ".nope").style.opacity =
        Math.min(Math.abs(delta) / 100, 1);
    },
    onReset: () => {
      dialog.style.transition = "";
      dialog.style.transform = "";
      dialog
        .querySelectorAll(".stamp")
        .forEach((stamp) => (stamp.style.opacity = 0));
    },
    onSwipe,
  });
}

// enableHorizontalSwipe manages a cancellation-safe horizontal pointer gesture.
function enableHorizontalSwipe(
  target,
  { onMove, onReset, onSwipe, suppressClick = false },
) {
  let pointerID;
  let start = 0;
  let delta = 0;
  let preventClick = false;

  const cancel = (event) => {
    if (event.pointerId !== pointerID) return;
    pointerID = undefined;
    delta = 0;
    onReset();
  };

  target.onpointerdown = (event) => {
    if (
      pointerID !== undefined ||
      !event.isPrimary ||
      (event.pointerType === "mouse" && event.button !== 0) ||
      event.target.closest("button, a, input, select, textarea")
    ) {
      return;
    }
    pointerID = event.pointerId;
    start = event.clientX;
    delta = 0;
    preventClick = false;
    target.setPointerCapture(pointerID);
  };
  target.onpointermove = (event) => {
    if (event.pointerId !== pointerID) return;
    delta = event.clientX - start;
    if (Math.abs(delta) > 6) preventClick = true;
    onMove(delta);
  };
  target.onpointerup = (event) => {
    if (event.pointerId !== pointerID) return;
    pointerID = undefined;
    if (Math.abs(delta) > 90) {
      onSwipe(delta > 0);
      return;
    }
    onReset();
  };
  target.onpointercancel = cancel;
  target.onlostpointercapture = cancel;

  if (suppressClick) {
    target.addEventListener(
      "click",
      (event) => {
        if (!preventClick) return;
        event.stopImmediatePropagation();
        preventClick = false;
      },
      { capture: true },
    );
  }
}

// setPoster loads an item poster and substitutes the ScreenDeck mark after failures.
export function setPoster(image, item, decorative = false) {
  image.onerror = () => {
    image.onerror = null;
    image.classList.add("poster-fallback");
    image.src = "/favicon.svg";
    if (!decorative) image.alt = `Poster unavailable for ${item.title}`;
  };
  image.src = `/api/posters/${encodeURIComponent(item.id)}`;
  image.alt = decorative ? "" : `Poster for ${item.title}`;
}
