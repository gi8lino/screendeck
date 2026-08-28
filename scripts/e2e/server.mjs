import http from "node:http";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(scriptDir, "../..");
const webRoot = path.join(root, "web", "dist");
const roomCode = "DECK42";

const args = new Map();
for (let i = 2; i < process.argv.length; i += 2) {
  args.set(process.argv[i], process.argv[i + 1]);
}

const host = args.get("--host") || "127.0.0.1";
const port = Number(args.get("--port") || 18081);

const items = [
  media("signal", "Signal at Perihelion", 2024, 8.1),
  media("parallax", "Parallax Station", 2023, 8.4),
  media("kepler", "Night Shift at Kepler-9", 2022, 7.9),
];

function media(id, title, year, rating) {
  return {
    id,
    libraryKey: "movies",
    type: "movie",
    guid: `e2e://${id}`,
    title,
    year,
    summary: `Deterministic browser-test fixture for ${title}.`,
    duration: 6600000,
    rating,
    genres: ["Drama", "Science Fiction"],
    viewed: false,
    addedAt: 1700000000,
  };
}

let state;

function resetState() {
  for (const response of state?.eventClients || []) response.end();
  state = {
    mediaConfigured: false,
    roomCreated: false,
    round: 1,
    activeItemIDs: items.map((item) => item.id),
    participants: new Map(),
    votes: new Map(),
    ready: new Set(),
    eventClients: new Set(),
    eventConnections: 0,
  };
}
resetState();

function sendJSON(response, status, value) {
  const body = JSON.stringify(value);
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
  });
  response.end(body);
}

async function readJSON(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  if (!chunks.length) return {};
  return JSON.parse(Buffer.concat(chunks).toString("utf8"));
}

function participant(token) {
  return state.participants.get(token);
}

function publicParticipants() {
  return [...state.participants.values()].map((entry) => ({
    ...entry,
    readyForNextRound: state.ready.has(entry.token),
    token: undefined,
  }));
}

function matchingItems() {
  const participants = [...state.participants.keys()];
  if (participants.length < 2) return [];
  return state.activeItemIDs
    .filter((itemID) =>
      participants.every((token) => state.votes.get(token)?.get(itemID) === true),
    )
    .map((itemID) => items.find((item) => item.id === itemID));
}

function roomState(token) {
  const me = participant(token);
  if (!me) return null;
  const votes = state.votes.get(token) || new Map();
  const candidateID = state.activeItemIDs.find((itemID) => !votes.has(itemID));
  const matches = matchingItems();
  const participants = publicParticipants();
  const requester = [...state.ready][0];
  const requestedBy = requester
    ? participants.find((entry) => entry.id === participant(requester)?.id)
    : undefined;

  return {
    room: {
      code: roomCode,
      round: state.round,
      phase: state.ready.size ? "next_round_requested" : "swiping",
      ownerId: "host",
      createdAt: "2026-08-28T08:00:00Z",
      expiresAt: "2026-08-29T08:00:00Z",
      locked: false,
    },
    me: participants.find((entry) => entry.id === me.id),
    participants,
    candidate: candidateID
      ? items.find((item) => item.id === candidateID)
      : undefined,
    posterLookahead: state.activeItemIDs
      .filter((itemID) => itemID !== candidateID && !votes.has(itemID))
      .slice(0, 2),
    matches,
    progress: {
      voted: votes.size,
      total: state.activeItemIDs.length,
      roundTotal: state.activeItemIDs.length,
      filteredOut: 0,
    },
    nextRound: {
      ready: state.ready.size,
      required: state.participants.size,
      available: matches.length >= 2,
      ...(requestedBy ? { requestedBy } : {}),
    },
    roundComplete: votes.size === state.activeItemIDs.length,
    moreTitles: { available: 0, canAdd: false },
  };
}

function addParticipant(token, id, name, isHost) {
  state.participants.set(token, {
    token,
    id,
    name,
    genres: [],
    genreMode: "any",
    isHost,
  });
  state.votes.set(token, new Map());
}

function notifyRoom() {
  for (const response of state.eventClients) {
    response.write("event: update\ndata: changed\n\n");
  }
}

function requireRoomToken(request, response) {
  const token = String(request.headers["x-participant-token"] || "");
  if (!participant(token)) {
    sendJSON(response, 403, { error: "forbidden" });
    return "";
  }
  return token;
}

function serveEvents(request, response) {
  const token = requireRoomToken(request, response);
  if (!token) return;

  state.eventConnections += 1;
  response.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  response.write("event: update\ndata: connected\n\n");
  state.eventClients.add(response);

  const heartbeat = setInterval(() => response.write(": keepalive\n\n"), 15000);
  request.on("close", () => {
    clearInterval(heartbeat);
    state.eventClients.delete(response);
  });
}

function servePoster(pathname, response) {
  const id = decodeURIComponent(pathname.slice("/api/posters/".length));
  const item = items.find((candidate) => candidate.id === id);
  if (!item) {
    sendJSON(response, 404, { error: "not found" });
    return;
  }
  const title = item.title.replace(/[<>&]/g, "");
  const body = `<svg xmlns="http://www.w3.org/2000/svg" width="360" height="540"><rect width="100%" height="100%" fill="#111014"/><text x="50%" y="50%" fill="#f7f3ed" font-size="24" text-anchor="middle">${title}</text></svg>`;
  response.writeHead(200, {
    "Content-Type": "image/svg+xml",
    "Content-Length": Buffer.byteLength(body),
  });
  response.end(body);
}

async function handleE2E(request, response, url) {
  if (url.pathname === "/__e2e/reset" && request.method === "POST") {
    resetState();
    sendJSON(response, 200, { status: "reset" });
    return true;
  }
  if (url.pathname === "/__e2e/configure" && request.method === "POST") {
    state.mediaConfigured = true;
    sendJSON(response, 200, { status: "configured" });
    return true;
  }
  if (url.pathname === "/__e2e/drop-events" && request.method === "POST") {
    for (const client of [...state.eventClients]) client.end();
    sendJSON(response, 200, { status: "dropped" });
    return true;
  }
  if (url.pathname === "/__e2e/status" && request.method === "GET") {
    sendJSON(response, 200, {
      eventConnections: state.eventConnections,
      activeEvents: state.eventClients.size,
      participants: state.participants.size,
      round: state.round,
    });
    return true;
  }
  return false;
}

async function handleAPI(request, response, url) {
  const route = `${request.method} ${url.pathname}`;

  switch (route) {
    case "GET /healthz":
      sendJSON(response, 200, { status: "ok" });
      return true;
    case "GET /api/config":
      sendJSON(response, 200, {
        version: "e2e",
        commit: "browser-tests",
        baseUrl: `http://${host}:${port}`,
        experimental: false,
        networkWarning: false,
        mediaConfigured: state.mediaConfigured,
        mediaProvider: state.mediaConfigured ? "jellyfin" : "",
        mediaServerName: state.mediaConfigured ? "E2E Jellyfin" : "",
      });
      return true;
    case "POST /api/jellyfin/connect":
      state.mediaConfigured = true;
      sendJSON(response, 200, { status: "connected" });
      return true;
    case "GET /api/me/rooms":
      sendJSON(response, 200, []);
      return true;
    case "GET /api/libraries":
      sendJSON(response, 200, [
        { key: "movies", title: "Movies", type: "movie" },
      ]);
      return true;
    case "POST /api/catalog/options":
      sendJSON(response, 200, {
        genres: ["Drama", "Science Fiction"],
        minYear: 2022,
        maxYear: 2024,
      });
      return true;
    case "POST /api/rooms": {
      if (!state.mediaConfigured) {
        sendJSON(response, 503, { error: "media provider not configured" });
        return true;
      }
      const input = await readJSON(request);
      state.roomCreated = true;
      state.round = 1;
      state.activeItemIDs = items.map((item) => item.id);
      state.participants.clear();
      state.votes.clear();
      state.ready.clear();
      addParticipant("host-token", "host", input.name || "Host", true);
      sendJSON(response, 201, { code: roomCode, token: "host-token" });
      return true;
    }
    case "POST /api/rooms/join": {
      if (!state.roomCreated) {
        sendJSON(response, 404, { error: "room not found" });
        return true;
      }
      const input = await readJSON(request);
      addParticipant("guest-token", "guest", input.name || "Guest", false);
      notifyRoom();
      sendJSON(response, 201, { code: roomCode, token: "guest-token" });
      return true;
    }
    case `GET /api/rooms/${roomCode}/genres`:
      sendJSON(response, 200, { genres: ["Drama", "Science Fiction"] });
      return true;
    case `GET /api/rooms/${roomCode}`: {
      const token = requireRoomToken(request, response);
      if (!token) return true;
      sendJSON(response, 200, roomState(token));
      return true;
    }
    case `GET /api/rooms/${roomCode}/events`:
      serveEvents(request, response);
      return true;
    case `POST /api/rooms/${roomCode}/votes`: {
      const token = requireRoomToken(request, response);
      if (!token) return true;
      const input = await readJSON(request);
      const before = matchingItems().some((item) => item.id === input.itemId);
      state.votes.get(token).set(input.itemId, Boolean(input.liked));
      const after = matchingItems().some((item) => item.id === input.itemId);
      notifyRoom();
      sendJSON(response, 200, { matched: !before && after });
      return true;
    }
    case `POST /api/rooms/${roomCode}/round-ready`: {
      const token = requireRoomToken(request, response);
      if (!token) return true;
      const input = await readJSON(request);
      if (input.ready) state.ready.add(token);
      else state.ready.delete(token);

      const matches = matchingItems();
      let advanced = false;
      let titles = matches.length;
      if (
        input.ready &&
        state.ready.size === state.participants.size &&
        matches.length >= 2
      ) {
        state.round += 1;
        state.activeItemIDs = matches.map((item) => item.id);
        for (const participantToken of state.participants.keys()) {
          state.votes.set(participantToken, new Map());
        }
        state.ready.clear();
        advanced = true;
        titles = state.activeItemIDs.length;
      }
      notifyRoom();
      sendJSON(response, 200, {
        round: state.round,
        titles,
        ready: state.ready.size,
        required: state.participants.size,
        advanced,
      });
      return true;
    }
    default:
      break;
  }

  if (request.method === "GET" && url.pathname.startsWith("/api/posters/")) {
    servePoster(url.pathname, response);
    return true;
  }

  if (url.pathname.startsWith("/api/")) {
    sendJSON(response, 404, { error: "not found" });
    return true;
  }
  return false;
}

const contentTypes = new Map([
  [".html", "text/html; charset=utf-8"],
  [".css", "text/css; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".mjs", "text/javascript; charset=utf-8"],
  [".svg", "image/svg+xml"],
]);

async function serveStatic(pathname, response) {
  const relative = pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
  const filePath = path.resolve(webRoot, relative);
  if (
    !filePath.startsWith(`${webRoot}${path.sep}`) &&
    filePath !== path.join(webRoot, "index.html")
  ) {
    response.writeHead(403);
    response.end("forbidden");
    return;
  }

  try {
    const body = await fs.readFile(filePath);
    response.writeHead(200, {
      "Content-Type":
        contentTypes.get(path.extname(filePath)) || "application/octet-stream",
    });
    response.end(body);
  } catch {
    const body = await fs.readFile(path.join(webRoot, "index.html"));
    response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    response.end(body);
  }
}

const server = http.createServer(async (request, response) => {
  try {
    const url = new URL(request.url, `http://${request.headers.host || `${host}:${port}`}`);
    if (await handleE2E(request, response, url)) return;
    if (await handleAPI(request, response, url)) return;
    await serveStatic(url.pathname, response);
  } catch (error) {
    console.error(error);
    if (!response.headersSent) response.writeHead(500);
    response.end("internal server error");
  }
});

try {
  await fs.access(path.join(webRoot, "index.html"));
} catch {
  console.error(`ScreenDeck frontend not found: ${webRoot}`);
  process.exit(1);
}

server.listen(port, host, () => {
  process.stdout.write(`ScreenDeck e2e server listening on http://${host}:${port}\n`);
});
