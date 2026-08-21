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
    candidate: items.arrival,
    matches: [items.dune, items.knives, items.severance, items.expanse],
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
    matches: [items.arrival],
    winner: {
      item: items.arrival,
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

const server = http.createServer(async (request, response) => {
  try {
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

    if (request.method === "GET" && url.pathname === "/api/me/rooms") {
      sendJSON(response, 200, [
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
      ]);
      return;
    }

    if (
      request.method === "POST" &&
      url.pathname === `/api/me/rooms/${roomCode}/session`
    ) {
      sendJSON(response, 200, {
        code: roomCode,
        token: "demo-host",
      });
      return;
    }

    if (
      request.method === "POST" &&
      url.pathname === `/api/me/rooms/${roomCode}/claim`
    ) {
      sendJSON(response, 200, { status: "claimed" });
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
      return;
    }

    if (request.method === "GET" && url.pathname === "/demo/home") {
      demoPage(response);
      return;
    }

    if (request.method === "GET" && url.pathname === "/demo/host") {
      demoPage(response, "demo-host");
      return;
    }

    if (request.method === "GET" && url.pathname === "/demo/alice") {
      demoPage(response, "demo-alice");
      return;
    }

    if (request.method === "GET" && url.pathname === "/demo/bob") {
      demoPage(response, "demo-bob");
      return;
    }

    if (request.method === "GET" && url.pathname === "/demo/winner") {
      demoPage(response, "demo-winner");
      return;
    }

    await serveStatic(url.pathname, response);
  } catch (error) {
    console.error(error);

    if (!response.headersSent) {
      response.writeHead(500, {
        "Content-Type": "text/plain; charset=utf-8",
      });
    }

    response.end("internal server error");
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
