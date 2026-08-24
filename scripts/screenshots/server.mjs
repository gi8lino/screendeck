import http from "node:http";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(scriptDir, "../..");
const webRoot = path.join(root, "web", "dist");
const posterRoot = path.join(scriptDir, "posters");
const roomCode = "DECK42";

const args = new Map();
for (let i = 2; i < process.argv.length; i += 2) {
  args.set(process.argv[i], process.argv[i + 1]);
}

const host = args.get("--host") || "127.0.0.1";
const port = Number(args.get("--port") || 18080);

const participants = [
  {
    id: "host",
    name: "Host",
    genres: ["Drama", "Science Fiction"],
    genreMode: "any",
    isHost: true,
    readyForNextRound: true,
  },
  {
    id: "alice",
    name: "Alice",
    genres: ["Drama", "Mystery"],
    genreMode: "any",
    isHost: false,
    readyForNextRound: true,
  },
  {
    id: "bob",
    name: "Bob",
    genres: ["Science Fiction", "Thriller"],
    genreMode: "any",
    isHost: false,
    readyForNextRound: false,
  },
];

const items = {
  signal: media(
    "signal",
    "Signal at Perihelion",
    2024,
    "movie",
    8.1,
    ["Science Fiction", "Mystery"],
    7080000,
  ),
  "cold-boot": media(
    "cold-boot",
    "Cold Boot Horizon",
    2021,
    "movie",
    7.6,
    ["Science Fiction", "Thriller"],
    6240000,
  ),
  parallax: media(
    "parallax",
    "Parallax Station",
    2023,
    "show",
    8.4,
    ["Drama", "Mystery", "Science Fiction"],
    0,
  ),
  kepler: media(
    "kepler",
    "Night Shift at Kepler-9",
    2022,
    "show",
    7.9,
    ["Comedy", "Science Fiction"],
    0,
  ),
};

const summaries = {
  signal:
    "At a solar observatory on the edge of survivable space, a buried transmission begins answering questions nobody has asked.",
  "cold-boot":
    "A systems engineer crosses a frozen colony to restart its planetary computer before the final reserve power disappears.",
  parallax:
    "Investigators aboard an orbital station discover that every witness remembers a different version of the same impossible crime.",
  kepler:
    "Four underqualified technicians keep a remote research outpost running through strange failures, bad coffee, and very long nights.",
};

for (const item of Object.values(items)) {
  item.summary = summaries[item.id];
}

function media(id, title, year, type, rating, genres, duration) {
  return {
    id,
    libraryKey: type === "show" ? "2" : "1",
    type,
    guid: `demo://${id}`,
    title,
    year,
    summary: "",
    duration,
    rating,
    genres,
    viewed: false,
    addedAt: 1700000000,
  };
}

function participantFor(token) {
  if (token === "demo-alice") return participants[1];
  if (token === "demo-bob") return participants[2];
  return participants[0];
}

function activeState(token) {
  return {
    room: {
      code: roomCode,
      round: 1,
      phase: "next_round_requested",
      ownerId: "host",
      createdAt: "2026-08-20T07:30:00Z",
      expiresAt: "2026-08-21T07:30:00Z",
    },
    me: participantFor(token),
    participants,
    candidate: items.signal,
    matches: [items["cold-boot"], items.parallax, items.kepler],
    progress: {
      voted: 74,
      total: 250,
      roundTotal: 250,
      filteredOut: 0,
    },
    nextRound: {
      ready: 2,
      required: 3,
      available: true,
      requestedBy: participants[0],
    },
    roundComplete: false,
    moreTitles: {
      available: 432,
      canAdd: true,
    },
  };
}

function winnerState() {
  const roster = participants.map((participant) => ({
    ...participant,
    readyForNextRound: false,
  }));

  return {
    room: {
      code: roomCode,
      round: 3,
      phase: "finished",
      ownerId: "host",
      createdAt: "2026-08-20T07:30:00Z",
      expiresAt: "2026-08-21T07:30:00Z",
    },
    me: roster[0],
    participants: roster,
    matches: [items.parallax],
    winner: {
      item: items.parallax,
      likedBy: roster,
    },
    progress: {
      voted: 1,
      total: 1,
      roundTotal: 1,
      filteredOut: 0,
    },
    nextRound: {
      ready: 0,
      required: 3,
      available: false,
    },
    roundComplete: true,
    moreTitles: {
      available: 0,
      canAdd: false,
    },
  };
}

function sendJSON(response, status, value) {
  const body = JSON.stringify(value);

  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
  });
  response.end(body);
}

function demoPage(response, token = "") {
  const destination = token ? `/?room=${roomCode}` : "/";
  const session = JSON.stringify({
    code: roomCode,
    token,
  });
  const sessionScript = token
    ? `localStorage.setItem("screendeck.session", ${JSON.stringify(session)});`
    : `localStorage.removeItem("screendeck.session");`;

  response.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
  });

  response.end(
    `<!doctype html><meta charset="utf-8"><script>${sessionScript}location.replace(${JSON.stringify(destination)});</script>`,
  );
}

// actionDemoPage loads the real frontend and opens a deterministic home action.
async function actionDemoPage(response, buttonLabel) {
  const index = await fs.readFile(path.join(webRoot, "index.html"), "utf8");
  const openAction = `<script>
    const observer = new MutationObserver(() => {
      const button = [...document.querySelectorAll("button")]
        .find((candidate) => candidate.textContent.trim() === ${JSON.stringify(buttonLabel)});
      if (!button) return;
      observer.disconnect();
      button.click();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
  </script>`;
  const body = index.replace("</body>", `${openAction}</body>`);

  response.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
  });
  response.end(body);
}

// matchesDemoPage opens the real room view and expands its fictional match pile.
async function matchesDemoPage(response) {
  const index = await fs.readFile(path.join(webRoot, "index.html"), "utf8");
  const session = JSON.stringify({ code: roomCode, token: "demo-host" });
  const openMatches = `<script>
    localStorage.setItem("screendeck.session", ${JSON.stringify(session)});
    history.replaceState(null, "", "/?room=${roomCode}");
    const observer = new MutationObserver(() => {
      const pile = document.querySelector(".match-pile");
      if (!pile) return;
      observer.disconnect();
      pile.click();
    });
    observer.observe(document.documentElement, { childList: true, subtree: true });
  </script>`;
  const body = index.replace("</body>", `${openMatches}</body>`);

  response.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
    "Content-Length": Buffer.byteLength(body),
  });
  response.end(body);
}

const contentTypes = new Map([
  [".html", "text/html; charset=utf-8"],
  [".css", "text/css; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".svg", "image/svg+xml"],
]);

async function serveStatic(pathname, response) {
  const relative =
    pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
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
    response.writeHead(200, {
      "Content-Type": "text/html; charset=utf-8",
    });
    response.end(body);
  }
}

const demoPages = new Map([
  ["/demo/home", ""],
  ["/demo/host", "demo-host"],
  ["/demo/alice", "demo-alice"],
  ["/demo/bob", "demo-bob"],
  ["/demo/winner", "demo-winner"],
]);

// demoConfig returns the deterministic public configuration fixture.
function demoConfig(mediaConfigured = true) {
  return {
    version: "demo",
    commit: "screenshots",
    baseUrl: `http://${host}:${port}`,
    experimental: false,
    mediaConfigured,
    mediaProvider: mediaConfigured ? "plex" : "",
    mediaServerName: mediaConfigured ? "ScreenDeck Demo Plex" : "",
  };
}

// savedRoomMemberships returns the room list shown on the screenshot home page.
function savedRoomMemberships() {
  return [
    {
      code: roomCode,
      round: 1,
      phase: "next_round_requested",
      participantId: "host",
      name: "Host",
      isHost: true,
      participantCount: participants.length,
      createdAt: "2026-08-20T07:30:00Z",
      expiresAt: "2026-08-21T07:30:00Z",
    },
  ];
}

// serveRoomEvents keeps the deterministic room event stream open for the frontend.
function serveRoomEvents(request, response) {
  response.writeHead(200, {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  });
  response.write("event: update\ndata: connected\n\n");

  const timer = setInterval(() => response.write(": keepalive\n\n"), 15000);
  request.on("close", () => clearInterval(timer));
}

// servePoster returns the local real poster fixture for a known media item.
async function servePoster(pathname, response) {
  const id = decodeURIComponent(pathname.slice("/api/posters/".length));
  if (!items[id]) {
    response.writeHead(404, {
      "Content-Type": "text/plain; charset=utf-8",
    });
    response.end("not found");
    return;
  }

  const poster = await fs.readFile(path.join(posterRoot, `${id}.jpg`));
  response.writeHead(200, {
    "Content-Type": "image/jpeg",
    "Cache-Control": "no-store",
  });
  response.end(poster);
}

// handleAPIRequest serves deterministic API fixtures used by the real frontend.
async function handleAPIRequest(request, response, url) {
  const route = `${request.method} ${url.pathname}`;

  switch (route) {
    case "GET /healthz":
      sendJSON(response, 200, { status: "ok" });
      return true;
    case "GET /api/config":
      sendJSON(
        response,
        200,
        demoConfig(
          !String(request.headers.referer || "").includes("/demo/media-setup"),
        ),
      );
      return true;
    case "GET /api/me/rooms":
      sendJSON(response, 200, savedRoomMemberships());
      return true;
    case "GET /api/libraries":
      sendJSON(response, 200, [
        { key: "1", title: "Movies", type: "movie" },
        { key: "2", title: "TV Shows", type: "show" },
      ]);
      return true;
    case "POST /api/catalog/options":
      sendJSON(response, 200, {
        genres: [
          "Adventure",
          "Comedy",
          "Crime",
          "Drama",
          "Mystery",
          "Science Fiction",
          "Thriller",
        ],
        minYear: 1985,
        maxYear: 2024,
      });
      return true;
    case `POST /api/me/rooms/${roomCode}/session`:
      sendJSON(response, 200, { code: roomCode, token: "demo-host" });
      return true;
    case `GET /api/rooms/${roomCode}/genres`:
      sendJSON(response, 200, {
        genres: [
          "Adventure",
          "Comedy",
          "Crime",
          "Drama",
          "Mystery",
          "Science Fiction",
          "Thriller",
        ],
      });
      return true;
    case `GET /api/rooms/${roomCode}`: {
      const token = String(request.headers["x-participant-token"] || "");
      sendJSON(
        response,
        200,
        token === "demo-winner" ? winnerState() : activeState(token),
      );
      return true;
    }
    case `GET /api/rooms/${roomCode}/events`:
      serveRoomEvents(request, response);
      return true;
    default:
      break;
  }

  if (request.method === "GET" && url.pathname.startsWith("/api/posters/")) {
    await servePoster(url.pathname, response);
    return true;
  }

  return false;
}

// handleDemoPage initializes the requested screenshot session before loading the app.
async function handleDemoPage(request, response, url) {
  if (request.method === "GET" && url.pathname === "/demo/create") {
    await actionDemoPage(response, "Create a room");
    return true;
  }
  if (request.method === "GET" && url.pathname === "/demo/media-setup") {
    await actionDemoPage(response, "Connect media server");
    return true;
  }
  if (request.method === "GET" && url.pathname === "/demo/matches") {
    await matchesDemoPage(response);
    return true;
  }
  if (request.method !== "GET" || !demoPages.has(url.pathname)) return false;

  demoPage(response, demoPages.get(url.pathname));
  return true;
}

// requestURL parses a request against the screenshot server origin.
function requestURL(request) {
  return new URL(
    request.url,
    `http://${request.headers.host || `${host}:${port}`}`,
  );
}

// handleRequest routes fixture API requests, demo entry points, and static assets.
async function handleRequest(request, response) {
  const url = requestURL(request);

  if (await handleAPIRequest(request, response, url)) return;
  if (await handleDemoPage(request, response, url)) return;

  await serveStatic(url.pathname, response);
}

// writeServerError records an unexpected fixture failure and completes the response.
function writeServerError(response, error) {
  console.error(error);

  if (!response.headersSent) {
    response.writeHead(500, {
      "Content-Type": "text/plain; charset=utf-8",
    });
  }
  response.end("internal server error");
}

const server = http.createServer(async (request, response) => {
  try {
    await handleRequest(request, response);
  } catch (error) {
    writeServerError(response, error);
  }
});

try {
  await fs.access(path.join(webRoot, "index.html"));
} catch {
  console.error(`ScreenDeck frontend not found: ${webRoot}`);
  process.exit(1);
}

server.on("error", (error) => {
  console.error(`Screenshot demo server failed: ${error.message}`);
  process.exit(1);
});

server.listen(port, host, () => {
  process.stdout.write(
    `ScreenDeck screenshot demo listening on http://${host}:${port}\n`,
  );
});
