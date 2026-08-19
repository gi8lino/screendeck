import { el } from "./ui.js";

// selectedGenres returns checked genre values from a genre chip container.
export function selectedGenres(container) {
  return [
    ...container.querySelectorAll('input[type="checkbox"]:checked'),
  ].map((input) => input.value);
}

// renderGenreChoices replaces a container with selectable genre chips.
export function renderGenreChoices(container, genres, selected = []) {
  const selectedSet = new Set(selected);
  container.replaceChildren();
  genres.forEach((genre) => {
    const label = el("label", "genre-chip");
    const input = el("input");
    input.type = "checkbox";
    input.value = genre;
    input.checked = selectedSet.has(genre);
    label.append(input, el("span", "", genre));
    container.append(label);
  });
}
