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
  arrival: media(
    "arrival",
    "Arrival",
    2016,
    "movie",
    7.9,
    ["Drama", "Science Fiction", "Mystery"],
    6960000,
  ),
  dune: media(
    "dune",
    "Dune",
    2021,
    "movie",
    8.0,
    ["Adventure", "Drama", "Science Fiction"],
    9300000,
  ),
  knives: media(
    "knives",
    "Knives Out",
    2019,
    "movie",
    7.9,
    ["Comedy", "Crime", "Mystery"],
    7860000,
  ),
  severance: media(
    "severance",
    "Severance",
    2022,
    "show",
    8.7,
    ["Drama", "Mystery", "Thriller"],
    0,
  ),
  expanse: media(
    "expanse",
    "The Expanse",
    2015,
    "show",
    8.5,
    ["Drama", "Science Fiction", "Thriller"],
    0,
  ),
};

const summaries = {
  arrival:
    "A linguist works with the military to communicate with mysterious visitors whose arrival could reshape humanity.",
  dune: "A young heir travels to a dangerous desert world where rival houses fight over its most valuable resource.",
  knives:
    "A detective untangles a family full of motives after a celebrated novelist dies under suspicious circumstances.",
  severance:
    "Office workers discover that separating work memories from personal life creates more questions than it answers.",
  expanse:
    "A missing-person case pulls strangers into a conspiracy that stretches across a colonized solar system.",
};
for (const item of Object.values(items)) item.summary = summaries[item.id];

const posterColors = {
  arrival: ["#263f57", "#7899aa"],
  dune: ["#6b4028", "#d39556"],
  knives: ["#4a2235", "#ae667a"],
  severance: ["#223638", "#709496"],
  expanse: ["#1e2949", "#5e75ab"],
};

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
    candidate: items.arrival,
    matches: [items.dune, items.knives, items.severance, items.expanse],
    progress: { voted: 74, total: 250, roundTotal: 250, filteredOut: 0 },
    nextRound: {
      ready: 2,
      required: 3,
      available: true,
      requestedBy: participants[0],
    },
    roundComplete: false,
    moreTitles: { available: 432, canAdd: true },
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
    matches: [items.arrival],
    winner: { item: items.arrival, likedBy: roster },
    progress: { voted: 1, total: 1, roundTotal: 1, filteredOut: 0 },
    nextRound: { ready: 0, required: 3, available: false },
    roundComplete: true,
    moreTitles: { available: 0, canAdd: false },
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

function escapeXML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

function posterSVG(item) {
  const [start, end] = posterColors[item.id] || ["#292731", "#5e596a"];
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 600 900">
  <defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${start}"/><stop offset="1" stop-color="${end}"/></linearGradient></defs>
  <rect width="600" height="900" fill="url(#g)"/>
  <circle cx="460" cy="165" r="185" fill="#fff" opacity=".08"/>
  <circle cx="120" cy="740" r="230" fill="#000" opacity=".14"/>
  <text x="58" y="90" fill="#fff" opacity=".7" font-family="system-ui,sans-serif" font-size="24" font-weight="700" letter-spacing="5">${item.type === "show" ? "TV SERIES" : "MOVIE"}</text>
  <text x="58" y="660" fill="#fff" font-family="system-ui,sans-serif" font-size="54" font-weight="800">${escapeXML(item.title)}</text>
  <text x="58" y="715" fill="#fff" opacity=".75" font-family="system-ui,sans-serif" font-size="28">${item.year}</text>
  <text x="58" y="820" fill="#fff" opacity=".52" font-family="system-ui,sans-serif" font-size="22">ScreenDeck demo</text>
</svg>`;
}

function demoSession(response, token) {
  const session = JSON.stringify({ code: roomCode, token });
  response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  response.end(
    `<!doctype html><meta charset="utf-8"><script>localStorage.setItem("screendeck.session", ${JSON.stringify(session)});location.replace("/");</script>`,
  );
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
    response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    response.end(body);
  }
}

const server = http.createServer(async (request, response) => {
  const url = new URL(
    request.url,
    `http://${request.headers.host || `${host}:${port}`}`,
  );

  if (request.method === "GET" && url.pathname === "/healthz") {
    sendJSON(response, 200, { status: "ok" });
    return;
  }
  if (request.method === "GET" && url.pathname === "/api/config") {
    sendJSON(response, 200, {
      version: "demo",
      commit: "screenshots",
      baseUrl: `http://${host}:${port}`,
      experimental: false,
      plexConfigured: true,
      plexServerName: "ScreenDeck Demo Plex",
    });
    return;
  }
  if (
    request.method === "GET" &&
    url.pathname === `/api/rooms/${roomCode}/genres`
  ) {
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
    return;
  }
  if (request.method === "GET" && url.pathname === `/api/rooms/${roomCode}`) {
    const token = String(request.headers["x-participant-token"] || "");
    sendJSON(
      response,
      200,
      token === "demo-winner" ? winnerState() : activeState(token),
    );
    return;
  }
  if (
    request.method === "GET" &&
    url.pathname === `/api/rooms/${roomCode}/events`
  ) {
    response.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });
    response.write("event: update\ndata: connected\n\n");
    const timer = setInterval(() => response.write(": keepalive\n\n"), 15000);
    request.on("close", () => clearInterval(timer));
    return;
  }
  if (request.method === "GET" && url.pathname.startsWith("/api/posters/")) {
    const id = decodeURIComponent(url.pathname.slice("/api/posters/".length));
    const item = items[id];
    if (!item) {
      response.writeHead(404);
      response.end("not found");
      return;
    }
    response.writeHead(200, { "Content-Type": "image/svg+xml" });
    response.end(posterSVG(item));
    return;
  }
  if (request.method === "GET" && url.pathname === "/demo/host") {
    demoSession(response, "demo-host");
    return;
  }
  if (request.method === "GET" && url.pathname === "/demo/alice") {
    demoSession(response, "demo-alice");
    return;
  }
  if (request.method === "GET" && url.pathname === "/demo/bob") {
    demoSession(response, "demo-bob");
    return;
  }
  if (request.method === "GET" && url.pathname === "/demo/winner") {
    demoSession(response, "demo-winner");
    return;
  }

  await serveStatic(url.pathname, response);
});

server.listen(port, host, () => {
  process.stdout.write(
    `ScreenDeck screenshot demo listening on http://${host}:${port}\n`,
  );
});
