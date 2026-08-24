import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import {
  backButton,
  instantiateTemplate,
  messageElement,
  root,
  showError,
  templateElement,
  topbar,
} from "./ui.js";

// renderCreateRoom displays the room creation form and media catalog filters.
export async function renderCreateRoom(navigation) {
  const view = createRoomView(navigation);
  const selectedLibraries = () => selectedLibraryKeys(view.form);

  await populateRoomLibraries(view.libraries, view.filters, selectedLibraries);
  view.form.onsubmit = (event) =>
    submitCreateRoom(event, navigation, view, selectedLibraries);
}

// createRoomView clones the static room-creation template and returns its controls.
function createRoomView(navigation) {
  const { fragment, refs } = instantiateTemplate("create-room-template");
  refs.yearTo.placeholder = String(new Date().getFullYear() + 5);

  root.replaceChildren(topbar(backButton(navigation.renderHome)), fragment);

  return {
    form: refs.form,
    name: refs.name,
    libraries: refs.libraries,
    error: refs.error,
    submit: refs.submit,
    filters: {
      personalGenres: refs.personalGenres,
      genreMode: refs.genreMode,
      status: refs.filterStatus,
      roundSize: refs.roundSize,
      samplingStrategy: refs.samplingStrategy,
      genres: refs.roomGenres,
      yearFrom: refs.yearFrom,
      yearTo: refs.yearTo,
      duration: refs.duration,
      watchedBox: refs.unwatchedOnly,
    },
  };
}

// selectedLibraryKeys returns the checked media library keys from a room form.
function selectedLibraryKeys(form) {
  return [...form.querySelectorAll("input[name=library]:checked")].map(
    (input) => input.value,
  );
}

// populateRoomLibraries loads media libraries and keeps catalog filters synchronized.
async function populateRoomLibraries(libraries, filters, selectedLibraries) {
  let filterTimer;
  const loadFilters = () => loadCatalogFilters(selectedLibraries(), filters);

  try {
    const available = await api("/api/libraries");
    renderLibraryChoices(available, libraries, () => {
      clearTimeout(filterTimer);
      filterTimer = setTimeout(loadFilters, 250);
    });
    await loadFilters();
  } catch (requestError) {
    libraries.replaceChildren(
      messageElement("notice-template", requestError.message),
    );
  }
}

// submitCreateRoom creates the room from the current form values.
async function submitCreateRoom(event, navigation, view, selectedLibraries) {
  event.preventDefault();
  view.error.textContent = "";
  view.submit.disabled = true;
  view.submit.textContent = "Building the deck…";

  try {
    const created = await api("/api/rooms", {
      method: "POST",
      body: JSON.stringify({
        name: view.name.value,
        libraryKeys: selectedLibraries(),
        genres: selectedGenres(view.filters.personalGenres),
        genreMode: view.filters.genreMode.value,
        roundSize: Number(view.filters.roundSize.value) || 0,
        samplingStrategy: view.filters.samplingStrategy.value,
        filters: filterValues(view.filters),
      }),
    });
    saveSession(created);
    await navigation.renderRoom();
  } catch (requestError) {
    showError(view.error, requestError);
    view.submit.disabled = false;
    view.submit.textContent = "Create room";
  }
}

// renderLibraryChoices displays selectable media libraries.
function renderLibraryChoices(libraries, container, onChange) {
  container.replaceChildren();
  if (!libraries.length) {
    container.append(
      messageElement("empty-template", "No movie or TV libraries found."),
    );
    return;
  }

  libraries.forEach((library, index) => {
    const { element: label, refs } = templateElement("library-choice-template");
    refs.input.value = library.key;
    refs.input.checked = index === 0;
    refs.input.onchange = onChange;
    refs.title.textContent = library.title;
    refs.type.textContent = library.type === "show" ? "TV" : "Movie";
    container.append(label);
  });
}

// loadCatalogFilters retrieves available genres and year boundaries.
async function loadCatalogFilters(libraryKeys, filters) {
  const selectedRoomGenres = selectedGenres(filters.genres);
  const selectedPersonalGenres = selectedGenres(filters.personalGenres);
  filters.genres.replaceChildren();
  filters.personalGenres.replaceChildren();
  if (!libraryKeys.length) {
    filters.status.textContent =
      "Choose at least one library to load its filters.";
    return;
  }
  filters.status.textContent = "Loading titles and available genres…";
  try {
    const options = await api("/api/catalog/options", {
      method: "POST",
      body: JSON.stringify({ libraryKeys }),
    });
    renderGenreChoices(filters.genres, options.genres, selectedRoomGenres);
    renderGenreChoices(
      filters.personalGenres,
      options.genres,
      selectedPersonalGenres,
    );
    filters.yearFrom.placeholder = options.minYear
      ? String(options.minYear)
      : "From";
    filters.yearTo.placeholder = options.maxYear
      ? String(options.maxYear)
      : "To";
    filters.status.textContent = `${options.genres.length} genres available · leave room filters empty to include everything.`;
  } catch (error) {
    filters.status.textContent = error.message;
  }
}

// filterValues returns normalized room filter values.
function filterValues(filters) {
  return {
    genres: selectedGenres(filters.genres),
    yearFrom: Number(filters.yearFrom.value) || 0,
    yearTo: Number(filters.yearTo.value) || 0,
    maxDurationMinutes: Number(filters.duration.value) || 0,
    unwatchedOnly: filters.watchedBox.checked,
  };
}
