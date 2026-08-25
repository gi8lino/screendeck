import { api } from "./api.js";
import { getConfig, setConfig } from "./state.js";
import {
  backButton,
  instantiateTemplate,
  root,
  showToast,
  templateElement,
  topbar,
} from "./ui.js";

// renderPlexSetup displays Plex authentication and server selection.
export function renderPlexSetup(navigation) {
  const view = createPlexSetupView(navigation);
  const start = (method) => startAuthorization(method, view, navigation);

  view.standardButton.onclick = () => start("standard");
  if (getConfig().experimental) {
    const { element: jwtButton } = templateElement("plex-jwt-button-template");
    jwtButton.onclick = () => start("jwt");
    view.authActions.append(jwtButton);
  }
}

// createPlexSetupView clones the static Plex authorization panel.
function createPlexSetupView(navigation) {
  const { fragment, refs } = instantiateTemplate("plex-setup-template");
  root.replaceChildren(
    topbar(backButton(navigation.renderMediaSetup || navigation.renderHome)),
    fragment,
  );
  return refs;
}

// startAuthorization runs one Plex authorization flow.
async function startAuthorization(method, view, navigation) {
  setAuthButtonsDisabled(view.authActions, true);
  view.status.textContent =
    method === "jwt"
      ? "Creating an experimental JWT authorization…"
      : "Creating a Plex authorization…";
  const popup = window.open("", "plex-auth", "width=720,height=760");

  try {
    const started = await api("/api/plex/auth", {
      method: "POST",
      body: JSON.stringify({ method }),
    });
    openAuthorization(started.authUrl, popup, view.servers);
    view.status.textContent = "Waiting for authorization in Plex…";

    const auth = await waitForAuthorization(started.setupToken);
    closePopup(popup);
    view.status.textContent = "Authorized. Choose your Plex Media Server.";
    view.authActions.remove();
    renderServers(
      auth.servers,
      started.setupToken,
      view.servers,
      view.status,
      navigation,
    );
  } catch (error) {
    closePopup(popup);
    view.status.textContent = error.message;
    setAuthButtonsDisabled(view.authActions, false);
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
  const { element: authLink } = templateElement("plex-auth-link-template");
  authLink.href = authURL;
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
    const { element: choice, refs } = templateElement("plex-server-template");
    refs.name.textContent = server.name;
    refs.details.textContent = [
      server.platform,
      server.local ? "Local connection" : "Remote connection",
      server.owned ? "Owned" : "Shared",
    ]
      .filter(Boolean)
      .join(" · ");
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
