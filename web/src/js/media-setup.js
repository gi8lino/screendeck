import { renderJellyfinSetup } from "./jellyfin.js";
import { renderPlexSetup } from "./plex.js";
import {
  backButton,
  instantiateTemplate,
  root,
  topbar,
} from "./ui.js";

// renderMediaSetup lets the user choose the media-server integration for this instance.
export function renderMediaSetup(navigation) {
  const { fragment, refs } = instantiateTemplate("media-setup-template");
  refs.plex.onclick = () => renderPlexSetup(navigation);
  refs.jellyfin.onclick = () => renderJellyfinSetup(navigation);
  root.replaceChildren(topbar(backButton(navigation.renderHome)), fragment);
}
