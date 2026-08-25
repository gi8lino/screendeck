import { api } from "./api.js";
import { setConfig } from "./state.js";
import {
  backButton,
  clearFieldErrors,
  instantiateTemplate,
  root,
  showError,
  showFieldErrors,
  showToast,
  topbar,
} from "./ui.js";

// renderJellyfinSetup displays the Jellyfin server and user login form.
export function renderJellyfinSetup(navigation) {
  const { fragment, refs } = instantiateTemplate("jellyfin-setup-template");
  refs.form.onsubmit = (event) => connectJellyfin(event, refs, navigation);
  root.replaceChildren(
    topbar(backButton(navigation.renderMediaSetup || navigation.renderHome)),
    fragment,
  );
}

// connectJellyfin authenticates ScreenDeck and persists the returned Jellyfin access token.
async function connectJellyfin(event, refs, navigation) {
  event.preventDefault();
  clearFieldErrors(refs.form);
  refs.error.textContent = "";
  refs.submit.disabled = true;
  refs.submit.textContent = "Connecting…";

  try {
    await api("/api/jellyfin/connect", {
      method: "POST",
      body: JSON.stringify({
        serverUrl: refs.serverUrl.value.trim(),
        username: refs.username.value.trim(),
        password: refs.password.value,
      }),
    });
    refs.password.value = "";
    setConfig(await api("/api/config"));
    showToast("Jellyfin connected");
    navigation.renderHome();
  } catch (error) {
    const rendered = showFieldErrors(refs.form, error, {
      serverUrl: refs.serverUrl,
      username: refs.username,
      password: refs.password,
    });
    if (rendered) {
      refs.error.textContent = "Please fix the highlighted fields.";
    } else {
      showError(refs.error, error);
    }
    refs.submit.disabled = false;
    refs.submit.textContent = "Connect Jellyfin";
  }
}
