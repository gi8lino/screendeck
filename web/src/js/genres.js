import { templateElement } from "./ui.js";

// selectedGenres returns checked genre values from a genre chip container.
export function selectedGenres(container) {
  return [...container.querySelectorAll('input[type="checkbox"]:checked')].map(
    (input) => input.value,
  );
}

// renderGenreChoices replaces a container with selectable genre chips.
export function renderGenreChoices(container, genres, selected = [], onChange) {
  const selectedSet = new Set(selected);
  container.replaceChildren();
  genres.forEach((genre) => {
    const { element: label, refs } = templateElement("genre-chip-template");
    refs.input.value = genre;
    refs.input.checked = selectedSet.has(genre);
    refs.input.onchange = onChange;
    refs.label.textContent = genre;
    container.append(label);
  });
}
