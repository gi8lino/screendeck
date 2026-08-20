import { api } from "./api.js";
import { renderGenreChoices, selectedGenres } from "./genres.js";
import { saveSession } from "./state.js";
import { backButton, el, root, showError, topbar } from "./ui.js";

// renderCreateRoom displays the room creation form and Plex filters.
export async function renderCreateRoom(navigation) {
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

  const libRow = el("div", "form-row");
  libRow.append(el("div", "label", "Movie and TV libraries"));
  const checks = el("div", "checks");
  checks.append(el("div", "empty", "Loading Plex libraries…"));
  libRow.append(checks);

  const filters = createFilterFields();
  const error = el("p", "error");
  const submit = el("button", "btn primary", "Create room");
  submit.type = "submit";
  form.append(nameRow, libRow, filters.personalBox, filters.box, error, submit);
  panel.append(form);
  root.append(panel);

  let filterTimer;
  const selectedLibraries = () =>
    [...form.querySelectorAll("input[name=library]:checked")].map(
      (input) => input.value,
    );
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

  form.onsubmit = async (event) => {
    event.preventDefault();
    error.textContent = "";
    submit.disabled = true;
    submit.textContent = "Building the deck…";
    try {
      const created = await api("/api/rooms", {
        method: "POST",
        body: JSON.stringify({
          name: name.value,
          libraryKeys: selectedLibraries(),
          genres: selectedGenres(filters.personalGenres),
          roundSize: Number(filters.roundSize.value) || 0,
          filters: filterValues(filters),
        }),
      });
      saveSession(created);
      await navigation.renderRoom();
    } catch (requestError) {
      showError(error, requestError);
      submit.disabled = false;
      submit.textContent = "Create room";
    }
  };
}

// createFilterFields creates personal genre and room filtering controls.
function createFilterFields() {
  const personalBox = el("section", "filter-box preference-box");
  personalBox.append(
    el("div", "label", "Your genres"),
    el(
      "p",
      "muted",
      "Optional · these only filter your own swipe deck. Leave empty to see every room title.",
    ),
  );
  const personalGenres = el("div", "genre-chips");
  personalGenres.setAttribute("role", "group");
  personalGenres.setAttribute("aria-label", "Your genres");
  personalBox.append(personalGenres);

  const box = el("section", "filter-box");
  box.append(el("div", "label", "Room filters"));
  const status = el(
    "p",
    "muted",
    "Choose at least one library to load its filters.",
  );
  const roundSizeRow = el("div", "form-row compact");
  roundSizeRow.append(el("label", "", "Maximum titles in the first round"));
  const roundSize = el("select");
  [
    ["50", "50 titles"],
    ["100", "100 titles"],
    ["250", "250 titles · recommended"],
    ["500", "500 titles"],
    ["0", "All matching titles"],
  ].forEach(([value, label]) => {
    const option = el("option", "", label);
    option.value = value;
    if (value === "250") option.selected = true;
    roundSize.append(option);
  });
  roundSizeRow.append(
    roundSize,
    el(
      "p",
      "muted",
      "Applied after room filters and shuffling. You can ask the group to narrow to the current matches at any time.",
    ),
  );

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
  box.append(status, roundSizeRow, genreRow, range, watched);
  return {
    personalBox,
    personalGenres,
    box,
    status,
    roundSize,
    genres,
    yearFrom,
    yearTo,
    duration,
    watchedBox,
  };
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
