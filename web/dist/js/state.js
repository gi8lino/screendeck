const sessionKey = "screendeck.session";

let config = {};
let session = readSession();

// readSession restores a participant session from browser storage.
function readSession() {
  try {
    return JSON.parse(localStorage.getItem(sessionKey)) || null;
  } catch {
    return null;
  }
}

// getConfig returns the current public application configuration.
export function getConfig() {
  return config;
}

// setConfig replaces the current public application configuration.
export function setConfig(value) {
  config = value || {};
}

// getSession returns the current participant session.
export function getSession() {
  return session;
}

// saveSession persists or clears the current participant session.
export function saveSession(value) {
  session = value;
  if (value) localStorage.setItem(sessionKey, JSON.stringify(value));
  else localStorage.removeItem(sessionKey);
}
