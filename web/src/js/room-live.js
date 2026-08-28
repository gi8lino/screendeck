import { getSession } from "./state.js";
import { root, showToast } from "./ui.js";

const reconnectIndicatorDelay = 5000;
const reconnectWarningDelay = 30000;
const reconnectDelay = 3000;

let roomEventController;
let roomViewGeneration = 0;
let roomConnectionState = "connecting";
let roomConnectionIndicatorVisible = false;
let reconnectIndicatorTimer;
let reconnectWarningTimer;

// roomGeneration identifies the currently active room-view lifecycle.
export function roomGeneration() {
  return roomViewGeneration;
}

// stopLiveRoomEvents closes the active event stream and invalidates stale room work.
export function stopLiveRoomEvents() {
  roomViewGeneration += 1;
  roomEventController?.abort();
  roomEventController = null;
  clearReconnectTimers();
  roomConnectionState = "connecting";
  roomConnectionIndicatorVisible = false;
}

// renderConnectionState displays connection progress only while live updates are unavailable.
export function renderConnectionState(node) {
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

// connectRoomEvents subscribes to changes from other room participants.
export async function connectRoomEvents({ onUpdate, isVoting }) {
  const session = getSession();
  if (!session || roomEventController) return;

  const generation = roomViewGeneration;
  const controller = new AbortController();
  roomEventController = controller;
  if (roomConnectionState !== "reconnecting") {
    setRoomConnectionState("connecting");
  }

  try {
    const response = await fetch(
      `/api/rooms/${encodeURIComponent(session.code)}/events`,
      {
        headers: {
          Accept: "text/event-stream",
          "X-Participant-Token": session.token,
        },
        signal: controller.signal,
      },
    );
    if (!response.ok || !response.body) {
      throw new Error(`Live updates failed (${response.status})`);
    }
    if (
      generation !== roomViewGeneration ||
      controller !== roomEventController
    ) {
      return;
    }
    setRoomConnectionState("connected");

    await consumeRoomEvents(response.body, controller, generation, {
      onUpdate,
      isVoting,
    });
  } catch (error) {
    if (error.name !== "AbortError") {
      console.debug("Room live updates interrupted; reconnecting.", error);
    }
  } finally {
    if (controller !== roomEventController) return;
    roomEventController = null;
    if (controller.signal.aborted || generation !== roomViewGeneration) return;
    setRoomConnectionState("reconnecting");
    setTimeout(() => {
      if (generation === roomViewGeneration) {
        void connectRoomEvents({ onUpdate, isVoting });
      }
    }, reconnectDelay);
  }
}

// consumeRoomEvents parses the data fields from a server-sent event stream.
async function consumeRoomEvents(
  stream,
  controller,
  generation,
  { onUpdate, isVoting },
) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffered = "";
  let eventData = [];

  while (!controller.signal.aborted) {
    const { value, done } = await reader.read();
    buffered += decoder.decode(value, { stream: !done });

    let lineEnd;
    while ((lineEnd = buffered.indexOf("\n")) >= 0) {
      const line = buffered.slice(0, lineEnd).replace(/\r$/, "");
      buffered = buffered.slice(lineEnd + 1);

      if (line.startsWith("data:")) {
        eventData.push(line.slice(5).trimStart());
      } else if (line === "") {
        const data = eventData.join("\n");
        eventData = [];
        if (
          generation === roomViewGeneration &&
          (data === "changed" || data === "connected") &&
          !isVoting()
        ) {
          void onUpdate();
        }
      }
    }

    if (done) return;
  }
}
