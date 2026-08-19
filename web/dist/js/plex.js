import { api } from "./api.js";
import { getConfig, setConfig } from "./state.js";
import { backButton, el, root, showToast, topbar } from "./ui.js";

// renderPlexSetup displays Plex authentication and server selection.
export function renderPlexSetup(navigation) {
  root.replaceChildren();
  root.append(topbar(backButton(navigation.renderHome)));
  const panel = el("section", "panel");
  panel.append(
    el("div", "eyebrow", "Plex connection"),
    el("h2", "", "Authorize this device."),
    el(
      "p",
      "lede",
      "Plex opens in a separate window. Sign in there, then return here to select the media server you want to use.",
    ),
  );
  const status = el("p", "notice", "Ready to connect.");
  const servers = el("div", "server-list");
  const authActions = el("div", "actions");
  const legacyButton = el("button", "btn primary", "Sign in with Plex");
  legacyButton.onclick = () => startAuthorization("legacy");
  authActions.append(legacyButton);
  if (getConfig().experimental) {
    const jwtButton = el("button", "btn ghost", "Use JWT (experimental)");
    jwtButton.onclick = () => startAuthorization("jwt");
    authActions.append(jwtButton);
  }
  panel.append(status, servers, authActions);
  root.append(panel);

  // startAuthorization runs one Plex authorization flow.
  async function startAuthorization(method) {
    setAuthButtonsDisabled(authActions, true);
    status.textContent =
      method === "jwt"
        ? "Creating an experimental JWT authorization…"
        : "Creating a Plex authorization…";
    const popup = window.open("", "plex-auth", "width=720,height=760");
    try {
      const started = await api("/api/plex/auth", {
        method: "POST",
        body: JSON.stringify({ method }),
      });
      openAuthorization(started.authUrl, popup, servers);
      status.textContent = "Waiting for authorization in Plex…";
      const auth = await waitForAuthorization(started.setupToken);
      closePopup(popup);
      status.textContent = "Authorized. Choose your Plex Media Server.";
      authActions.remove();
      renderServers(
        auth.servers,
        started.setupToken,
        servers,
        status,
        navigation,
      );
    } catch (error) {
      closePopup(popup);
      status.textContent = error.message;
      setAuthButtonsDisabled(authActions, false);
    }
  }
}

// setAuthButtonsDisabled toggles all authentication buttons.
function setAuthButtonsDisabled(actions, disabled) {
  actions
    .querySelectorAll("button")
    .forEach((button) => (button.disabled = disabled));
}

// openAuthorization opens Plex authentication or displays a fallback link.
function openAuthorization(authURL, popup, servers) {
  if (popup) {
    popup.location = authURL;
    return;
  }
  const authLink = el("a", "btn ghost", "Open Plex authorization");
  authLink.href = authURL;
  authLink.target = "_blank";
  authLink.rel = "noopener";
  servers.before(authLink);
}

// closePopup closes the Plex authentication window when possible.
function closePopup(popup) {
  try {
    popup?.close();
  } catch {
    /* cross-origin window */
  }
}

// waitForAuthorization polls Plex until the account authorizes the PIN.
async function waitForAuthorization(setupToken) {
  for (;;) {
    await new Promise((resolve) => setTimeout(resolve, 1400));
    const auth = await api("/api/plex/auth/status", {
      headers: { "X-Setup-Token": setupToken },
    });
    if (auth.status === "authorized") return auth;
  }
}

// renderServers displays discovered Plex servers.
function renderServers(available, setupToken, servers, status, navigation) {
  servers.replaceChildren();
  available.forEach((server) => {
    const choice = el("button", "server-choice");
    const details = el("span");
    details.append(
      el("strong", "", server.name),
      el(
        "small",
        "",
        [
          server.platform,
          server.local ? "Local connection" : "Remote connection",
          server.owned ? "Owned" : "Shared",
        ]
          .filter(Boolean)
          .join(" · "),
      ),
    );
    choice.append(details, el("span", "", "Connect →"));
    choice.onclick = () =>
      selectServer(server, setupToken, servers, status, navigation);
    servers.append(choice);
  });
}

// selectServer verifies and persists a selected Plex server.
async function selectServer(server, setupToken, servers, status, navigation) {
  servers
    .querySelectorAll("button")
    .forEach((button) => (button.disabled = true));
  status.textContent = `Verifying ${server.name}…`;
  try {
    await api("/api/plex/server", {
      method: "POST",
      headers: { "X-Setup-Token": setupToken },
      body: JSON.stringify({ serverId: server.id }),
    });
    setConfig(await api("/api/config"));
    showToast("Plex connected");
    navigation.renderHome();
  } catch (error) {
    status.textContent = error.message;
    servers
      .querySelectorAll("button")
      .forEach((button) => (button.disabled = false));
  }
}
