import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import {
  backButton,
  clearFieldErrors,
  instantiateTemplate,
  messageElement,
  root,
  showError,
  showFieldErrors,
  templateElement,
  topbar,
} from "./ui.js";

// renderCreateRoom displays the room creation form and media catalog filters.
export async function renderCreateRoom(navigation) {
  const view = createRoomView(navigation);
  const selectedLibraries = () => selectedLibraryKeys(view.form);
  wireFilterSummaries(view.filters);

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
    lifetimeHours: refs.lifetimeHours,
    filters: {
      personalGenres: refs.personalGenres,
      personalDetails: refs.personalDetails,
      personalSummary: refs.personalSummary,
      genreMode: refs.genreMode,
      status: refs.filterStatus,
      roomDetails: refs.roomDetails,
      roomSummary: refs.roomSummary,
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
  clearFieldErrors(view.form);
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
        lifetimeHours: Number(view.lifetimeHours.value) || 0,
        samplingStrategy: view.filters.samplingStrategy.value,
        filters: filterValues(view.filters),
      }),
    });
    saveSession(created);
    await navigation.renderRoom();
  } catch (requestError) {
    const rendered = showFieldErrors(view.form, requestError, {
      name: view.name,
      libraryKeys: view.libraries,
      genreMode: view.filters.genreMode,
      roundSize: view.filters.roundSize,
      lifetimeHours: view.lifetimeHours,
      samplingStrategy: view.filters.samplingStrategy,
      "filters.yearFrom": view.filters.yearFrom,
      "filters.yearTo": view.filters.yearTo,
      "filters.maxDurationMinutes": view.filters.duration,
    });
    if (rendered) {
      if (requestError.problems?.genreMode) {
        view.filters.personalDetails.open = true;
      }
      if (
        Object.keys(requestError.problems || {}).some((field) =>
          field.startsWith("filters."),
        )
      ) {
        view.filters.roomDetails.open = true;
      }
      view.error.textContent = "Please fix the highlighted fields.";
    } else {
      showError(view.error, requestError);
    }
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
    renderGenreChoices(filters.genres, options.genres, selectedRoomGenres, () =>
      updateFilterSummaries(filters),
    );
    renderGenreChoices(
      filters.personalGenres,
      options.genres,
      selectedPersonalGenres,
      () => updateFilterSummaries(filters),
    );
    filters.yearFrom.placeholder = options.minYear
      ? String(options.minYear)
      : "From";
    filters.yearTo.placeholder = options.maxYear
      ? String(options.maxYear)
      : "To";
    filters.status.textContent = `${options.genres.length} genres available · leave room filters empty to include everything.`;
    updateFilterSummaries(filters);
  } catch (error) {
    filters.status.textContent = error.message;
  }
}

// wireFilterSummaries keeps collapsed filter labels synchronized with their controls.
function wireFilterSummaries(filters) {
  for (const control of [
    filters.genreMode,
    filters.yearFrom,
    filters.yearTo,
    filters.duration,
    filters.watchedBox,
  ]) {
    control.addEventListener("input", () => updateFilterSummaries(filters));
    control.addEventListener("change", () => updateFilterSummaries(filters));
  }
  updateFilterSummaries(filters);
}

// updateFilterSummaries describes active personal and room filters while collapsed.
function updateFilterSummaries(filters) {
  const personalCount = selectedGenres(filters.personalGenres).length;
  filters.personalSummary.textContent = personalCount
    ? `${personalCount} selected`
    : "Optional";

  const roomGenres = selectedGenres(filters.genres).length;
  const summary = [];
  if (roomGenres) summary.push(`${roomGenres} genres`);
  if (filters.yearFrom.value) summary.push(`from ${filters.yearFrom.value}`);
  if (filters.yearTo.value) summary.push(`through ${filters.yearTo.value}`);
  if (filters.duration.value) summary.push(`≤ ${filters.duration.value} min`);
  if (filters.watchedBox.checked) summary.push("unwatched");
  filters.roomSummary.textContent = summary.length
    ? summary.join(" · ")
    : "None set";
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
