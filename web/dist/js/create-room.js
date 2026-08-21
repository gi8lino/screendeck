import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import { backButton, el, root, showError, topbar } from "./ui.js";

// renderCreateRoom displays the room creation form and Plex filters.
export async function renderCreateRoom(navigation) {
  const view = createRoomView(navigation);
  const selectedLibraries = () => selectedLibraryKeys(view.form);

  await populateRoomLibraries(view.checks, view.filters, selectedLibraries);
  view.form.onsubmit = (event) =>
    submitCreateRoom(event, navigation, view, selectedLibraries);
}

// createRoomView creates the static room-creation form and returns its controls.
function createRoomView(navigation) {
  root.replaceChildren();
  root.append(topbar(backButton(navigation.renderHome)));

  const panel = el("section", "panel");
  panel.append(
    el("div", "eyebrow", "Host a room"),
    el("h2", "", "Shape the watchlist."),
  );

  const form = el("form");
  const nameRow = el("div", "form-row");
  nameRow.append(el("label", "", "Your name"));
  const name = el("input");
  name.type = "text";
  name.maxLength = 30;
  name.required = true;
  name.placeholder = "Ripley";
  name.autocomplete = "nickname";
  nameRow.append(name);

  const libraryRow = el("div", "form-row");
  libraryRow.append(el("div", "label", "Movie and TV libraries"));
  const checks = el("div", "checks");
  checks.append(el("div", "empty", "Loading Plex libraries…"));
  libraryRow.append(checks);

  const filters = createFilterFields();
  const error = el("p", "error");
  const submit = el("button", "btn primary", "Create room");
  submit.type = "submit";
  form.append(
    nameRow,
    libraryRow,
    filters.personalBox,
    filters.box,
    error,
    submit,
  );
  panel.append(form);
  root.append(panel);

  return { form, name, checks, filters, error, submit };
}

// selectedLibraryKeys returns the checked Plex library keys from a room form.
function selectedLibraryKeys(form) {
  return [...form.querySelectorAll("input[name=library]:checked")].map(
    (input) => input.value,
  );
}

// populateRoomLibraries loads Plex libraries and keeps catalog filters synchronized.
async function populateRoomLibraries(checks, filters, selectedLibraries) {
  let filterTimer;
  const loadFilters = () => loadCatalogFilters(selectedLibraries(), filters);

  try {
    const libraries = await api("/api/libraries");
    renderLibraryChoices(libraries, checks, () => {
      clearTimeout(filterTimer);
      filterTimer = setTimeout(loadFilters, 250);
    });
    await loadFilters();
  } catch (requestError) {
    checks.replaceChildren(el("div", "notice", requestError.message));
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

// createFilterFields creates personal genre and room filtering controls.
function createFilterFields() {
  const personal = createPersonalGenreFields();
  const room = createRoomFilterFields();
  return {
    personalBox: personal.box,
    personalGenres: personal.genres,
    genreMode: personal.genreMode,
    box: room.box,
    status: room.status,
    roundSize: room.roundSize,
    samplingStrategy: room.samplingStrategy,
    genres: room.genres,
    yearFrom: room.yearFrom,
    yearTo: room.yearTo,
    duration: room.duration,
    watchedBox: room.watchedBox,
  };
}

// createPersonalGenreFields creates participant-specific genre preferences.
function createPersonalGenreFields() {
  const box = el("section", "filter-box preference-box");
  box.append(
    el("div", "label", "Your genres"),
    el(
      "p",
      "muted",
      "Optional · these only filter your own swipe deck. Leave empty to see every room title.",
    ),
  );

  const genreMode = el("select");
  const anyMode = el("option", "", "Match any selected genre");
  anyMode.value = "any";
  const allMode = el("option", "", "Match all selected genres");
  allMode.value = "all";
  genreMode.append(anyMode, allMode);

  const genres = el("div", "genre-chips");
  genres.setAttribute("role", "group");
  genres.setAttribute("aria-label", "Your genres");
  box.append(genreMode, genres);
  return { box, genres, genreMode };
}

// createRoomFilterFields creates shared catalog filters and first-round controls.
function createRoomFilterFields() {
  const box = el("section", "filter-box");
  box.append(el("div", "label", "Room filters"));

  const status = el(
    "p",
    "muted",
    "Choose at least one library to load its filters.",
  );
  const round = createRoundSelectionFields();
  const catalog = createCatalogFilterFields();

  box.append(
    status,
    round.sampling.row,
    round.roundSize.row,
    catalog.genreRow,
    catalog.range,
    catalog.watched,
  );
  return {
    box,
    status,
    samplingStrategy: round.sampling.select,
    roundSize: round.roundSize.select,
    genres: catalog.genres,
    yearFrom: catalog.yearFrom,
    yearTo: catalog.yearTo,
    duration: catalog.duration,
    watchedBox: catalog.watchedBox,
  };
}

// createRoundSelectionFields creates initial deck sizing and ordering controls.
function createRoundSelectionFields() {
  const sampling = selectField(
    "First-round selection",
    [
      ["random", "Random"],
      ["highest_rated", "Highest rated"],
      ["recently_added", "Recently added"],
      ["random_unwatched", "Random unwatched"],
    ],
    "Choose how ScreenDeck picks and orders titles before applying the first-round limit.",
  );
  const roundSize = selectField(
    "Maximum titles in the first round",
    [
      ["50", "50 titles"],
      ["100", "100 titles"],
      ["250", "250 titles · recommended"],
      ["500", "500 titles"],
      ["0", "All matching titles"],
    ],
    "Applied after room filters and shuffling. You can ask the group to narrow to the current matches at any time.",
    "250",
  );
  return { sampling, roundSize };
}

// createCatalogFilterFields creates shared genre, year, duration, and watched controls.
function createCatalogFilterFields() {
  const genreRow = el("div", "form-row compact");
  genreRow.append(el("label", "", "Room genres · select any that fit"));
  const genres = el("div", "genre-chips");
  genres.setAttribute("role", "group");
  genres.setAttribute("aria-label", "Room genres");
  genreRow.append(genres);

  const range = el("div", "range-grid");
  const yearFrom = numberField("From year", "1900");
  const yearTo = numberField("To year", String(new Date().getFullYear() + 5));
  const duration = numberField("Maximum movie length", "Minutes");
  range.append(yearFrom.row, yearTo.row, duration.row);

  const watched = el("label", "check");
  const watchedBox = el("input");
  watchedBox.type = "checkbox";
  watched.append(
    watchedBox,
    el("span", "", "Only include fully unwatched titles"),
  );

  return {
    genreRow,
    genres,
    range,
    yearFrom,
    yearTo,
    duration,
    watched,
    watchedBox,
  };
}

// selectField creates a labeled select row with explanatory copy.
function selectField(label, options, description, selectedValue = "") {
  const row = el("div", "form-row compact");
  row.append(el("label", "", label));

  const select = el("select");
  options.forEach(([value, text]) => {
    const option = el("option", "", text);
    option.value = value;
    if (value === selectedValue) option.selected = true;
    select.append(option);
  });
  row.append(select, el("p", "muted", description));
  return { row, select };
}

// numberField creates a labeled numeric input.
function numberField(label, placeholder) {
  const row = el("div", "mini-field");
  row.append(el("label", "", label));
  const input = el("input");
  input.type = "number";
  input.min = "0";
  input.placeholder = placeholder;
  row.append(input);
  return { row, input };
}

// renderLibraryChoices displays selectable Plex libraries.
function renderLibraryChoices(libraries, checks, onChange) {
  checks.replaceChildren();
  if (!libraries.length) {
    checks.append(el("div", "empty", "No movie or TV libraries found."));
  }
  libraries.forEach((library, index) => {
    const label = el("label", "check");
    const input = el("input");
    input.type = "checkbox";
    input.name = "library";
    input.value = library.key;
    input.checked = index === 0;
    input.onchange = onChange;
    const title = el("span", "", library.title);
    title.append(
      el("small", "library-type", library.type === "show" ? "TV" : "Movie"),
    );
    label.append(input, title);
    checks.append(label);
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
    filters.yearFrom.input.placeholder = options.minYear
      ? String(options.minYear)
      : "From";
    filters.yearTo.input.placeholder = options.maxYear
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
    yearFrom: Number(filters.yearFrom.input.value) || 0,
    yearTo: Number(filters.yearTo.input.value) || 0,
    maxDurationMinutes: Number(filters.duration.input.value) || 0,
    unwatchedOnly: filters.watchedBox.checked,
  };
}

