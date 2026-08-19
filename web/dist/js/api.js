import { getSession } from "./state.js";

// api sends a JSON request to the ScreenDeck API.
export async function api(path, options = {}) {
  const headers = {
    "Content-Type": "application/json",
    ...(options.headers || {}),
  };
  const session = getSession();
  if (session?.token) headers["X-Participant-Token"] = session.token;
  const response = await fetch(path, { ...options, headers });
  let payload = null;
  try {
    payload = await response.json();
  } catch {
    /* empty body */
  }
  if (!response.ok) {
    throw new Error(payload?.error || `Request failed (${response.status})`);
  }
  return payload;
}
