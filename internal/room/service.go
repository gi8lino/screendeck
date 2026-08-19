package room

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gi8lino/screendeck/internal/plex"
	"github.com/gi8lino/screendeck/internal/store"
)

type Service struct {
	store   *store.Store
	catalog Catalog
	roomTTL time.Duration
	mu      sync.Mutex
	events  map[string]map[chan struct{}]struct{}
	cache   map[string]cacheEntry
}

type Catalog interface {
	Libraries(context.Context) ([]plex.Library, error)
	Items(context.Context, plex.Library) ([]plex.Item, error)
	Poster(context.Context, string) (*http.Response, error)
}

type Filters struct {
	Genres             []string `json:"genres"`
	YearFrom           int      `json:"yearFrom"`
	YearTo             int      `json:"yearTo"`
	MaxDurationMinutes int      `json:"maxDurationMinutes"`
	UnwatchedOnly      bool     `json:"unwatchedOnly"`
}

type CatalogOptions struct {
	Genres  []string `json:"genres"`
	MinYear int      `json:"minYear"`
	MaxYear int      `json:"maxYear"`
}

type RoundResult struct {
	Round  int `json:"round"`
	Titles int `json:"titles"`
}

type cacheEntry struct {
	items     []plex.Item
	fetchedAt time.Time
}

type Session struct {
	Code  string `json:"code"`
	Token string `json:"token"`
}

// NewService creates a room service backed by the supplied catalog and store.
func NewService(database *store.Store, catalog Catalog, roomTTL time.Duration) *Service {
	return &Service{
		store:   database,
		catalog: catalog,
		roomTTL: roomTTL,
		events:  make(map[string]map[chan struct{}]struct{}),
		cache:   make(map[string]cacheEntry),
	}
}

// Libraries returns the catalog libraries sorted for display.
func (s *Service) Libraries(ctx context.Context) ([]plex.Library, error) {
	if s.catalog == nil {
		return nil, errors.New("Plex is not configured")
	}
	return s.catalog.Libraries(ctx)
}

// Options returns filter options found in the selected libraries.
func (s *Service) Options(ctx context.Context, libraryKeys []string) (CatalogOptions, error) {
	_, items, err := s.loadItems(ctx, libraryKeys)
	if err != nil {
		return CatalogOptions{}, err
	}
	genreSet := make(map[string]struct{})
	options := CatalogOptions{Genres: make([]string, 0)}
	for _, item := range items {
		for _, genre := range item.Genres {
			if strings.TrimSpace(genre) != "" {
				genreSet[genre] = struct{}{}
			}
		}
		if item.Year > 0 && (options.MinYear == 0 || item.Year < options.MinYear) {
			options.MinYear = item.Year
		}
		if item.Year > options.MaxYear {
			options.MaxYear = item.Year
		}
	}
	for genre := range genreSet {
		options.Genres = append(options.Genres, genre)
	}
	sort.Strings(options.Genres)
	return options, nil
}

// Create creates a room and joins its first participant.
func (s *Service) Create(ctx context.Context, name string, libraryKeys []string, filters Filters, genres []string) (Session, error) {
	name = cleanName(name)
	if name == "" {
		return Session{}, errors.New("name is required")
	}
	if len(libraryKeys) == 0 {
		return Session{}, errors.New("select at least one library")
	}
	if filters.YearFrom < 0 || filters.YearTo < 0 || filters.MaxDurationMinutes < 0 || (filters.YearFrom > 0 && filters.YearTo > 0 && filters.YearFrom > filters.YearTo) {
		return Session{}, errors.New("invalid catalog filters")
	}
	_, items, err := s.loadItems(ctx, libraryKeys)
	if err != nil {
		return Session{}, err
	}
	seen := make(map[string]bool)
	genreSet := make(map[string]struct{}, len(filters.Genres))
	for _, genre := range filters.Genres {
		if normalized := strings.ToLower(strings.TrimSpace(genre)); normalized != "" {
			genreSet[normalized] = struct{}{}
		}
	}
	var itemIDs []string
	availableGenreSet := make(map[string]string)
	for _, item := range items {
		if !matchesFilters(item, filters, genreSet) || seen[item.RatingKey] {
			continue
		}
		seen[item.RatingKey] = true
		itemIDs = append(itemIDs, item.RatingKey)
		collectGenres(availableGenreSet, item.Genres)
	}
	if len(itemIDs) == 0 {
		return Session{}, errors.New("the selected libraries contain no matching titles")
	}
	participantGenres, err := canonicalGenres(genres, genreValues(availableGenreSet))
	if err != nil {
		return Session{}, err
	}
	mathrand.Shuffle(len(itemIDs), func(i, j int) { itemIDs[i], itemIDs[j] = itemIDs[j], itemIDs[i] })
	code, err := roomCode()
	if err != nil {
		return Session{}, err
	}
	participantID, token, tokenHash, err := credentials()
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	err = s.store.CreateRoom(
		ctx,
		store.Room{Code: code, Round: 1, CreatedAt: now, ExpiresAt: now.Add(s.roomTTL)},
		store.Participant{ID: participantID, Name: name, Genres: participantGenres},
		tokenHash,
		itemIDs,
	)
	if err != nil {
		return Session{}, err
	}
	return Session{Code: code, Token: token}, nil
}

// loadItems resolves selected libraries and loads their media items.
func (s *Service) loadItems(ctx context.Context, libraryKeys []string) ([]plex.Library, []plex.Item, error) {
	if len(libraryKeys) == 0 {
		return nil, nil, errors.New("select at least one library")
	}
	libraries, err := s.Libraries(ctx)
	if err != nil {
		return nil, nil, err
	}
	allowed := make(map[string]plex.Library, len(libraries))
	for _, library := range libraries {
		allowed[library.Key] = library
	}
	selected := make([]plex.Library, 0, len(libraryKeys))
	var items []plex.Item
	for _, key := range libraryKeys {
		library, ok := allowed[key]
		if !ok {
			return nil, nil, fmt.Errorf("media library %q not found", key)
		}
		selected = append(selected, library)
		libraryItems, err := s.libraryItems(ctx, library)
		if err != nil {
			return nil, nil, fmt.Errorf("load %s: %w", library.Title, err)
		}
		items = append(items, libraryItems...)
	}
	return selected, items, nil
}

// libraryItems returns cached or freshly loaded items for a library.
func (s *Service) libraryItems(ctx context.Context, library plex.Library) ([]plex.Item, error) {
	s.mu.Lock()
	cached, ok := s.cache[library.Key]
	s.mu.Unlock()
	if ok && time.Since(cached.fetchedAt) < 5*time.Minute {
		return cached.items, nil
	}
	items, err := s.catalog.Items(ctx, library)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveLibrary(ctx, library, items); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[library.Key] = cacheEntry{items: items, fetchedAt: time.Now()}
	s.mu.Unlock()
	return items, nil
}

// matchesFilters reports whether a media item satisfies room filters.
func matchesFilters(item plex.Item, filters Filters, genres map[string]struct{}) bool {
	if filters.UnwatchedOnly && item.Viewed {
		return false
	}
	if filters.YearFrom > 0 && item.Year < filters.YearFrom {
		return false
	}
	if filters.YearTo > 0 && item.Year > filters.YearTo {
		return false
	}
	if filters.MaxDurationMinutes > 0 && item.Type == "movie" && item.Duration > filters.MaxDurationMinutes*60*1000 {
		return false
	}
	if len(genres) > 0 {
		matched := false
		for _, genre := range item.Genres {
			if _, ok := genres[strings.ToLower(strings.TrimSpace(genre))]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// Genres returns the genres currently represented in a room's deck.
func (s *Service) Genres(ctx context.Context, code string) ([]string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 6 {
		return nil, errors.New("a six-character room code is required")
	}
	return s.store.RoomGenres(ctx, code)
}

// Join adds a participant to an existing room.
func (s *Service) Join(ctx context.Context, code, name string, genres []string) (Session, error) {
	code, name = strings.ToUpper(strings.TrimSpace(code)), cleanName(name)
	if len(code) != 6 || name == "" {
		return Session{}, errors.New("a six-character room code and name are required")
	}
	availableGenres, err := s.store.RoomGenres(ctx, code)
	if err != nil {
		return Session{}, err
	}
	participantGenres, err := canonicalGenres(genres, availableGenres)
	if err != nil {
		return Session{}, err
	}
	participantID, token, tokenHash, err := credentials()
	if err != nil {
		return Session{}, err
	}
	if err := s.store.JoinRoom(ctx, code, store.Participant{ID: participantID, Name: name, Genres: participantGenres}, tokenHash); err != nil {
		return Session{}, err
	}
	s.Notify(code)
	return Session{Code: code, Token: token}, nil
}

// State returns a room state visible to an authenticated participant.
func (s *Service) State(ctx context.Context, code, token string) (store.RoomState, error) {
	participant, err := s.store.ParticipantByToken(ctx, strings.ToUpper(code), hashToken(token))
	if err != nil {
		return store.RoomState{}, err
	}
	return s.store.RoomState(ctx, strings.ToUpper(code), participant.ID)
}

// Vote records a participant vote and reports whether it produced a match.
func (s *Service) Vote(ctx context.Context, code, token, movieID string, liked bool) (bool, error) {
	code = strings.ToUpper(code)
	participant, err := s.store.ParticipantByToken(ctx, code, hashToken(token))
	if err != nil {
		return false, err
	}
	matched, err := s.store.Vote(ctx, code, participant.ID, movieID, liked)
	if err == nil {
		s.Notify(code)
	}
	return matched, err
}

// NextRound replaces the current deck with its matches and starts another round.
func (s *Service) NextRound(ctx context.Context, code, token string, expectedRound int) (RoundResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	participant, err := s.store.ParticipantByToken(ctx, code, hashToken(token))
	if err != nil {
		return RoundResult{}, err
	}
	round, titles, _, err := s.store.AdvanceRound(ctx, code, participant.ID, expectedRound)
	if err != nil {
		return RoundResult{}, err
	}
	s.Notify(code)
	return RoundResult{Round: round, Titles: titles}, nil
}

// Leave removes a participant from a room.
func (s *Service) Leave(ctx context.Context, code, token string) error {
	code = strings.ToUpper(code)
	if err := s.store.LeaveRoom(ctx, code, hashToken(token)); err != nil {
		return err
	}
	s.Notify(code)
	return nil
}

// Poster retrieves the poster associated with a stored media item.
func (s *Service) Poster(ctx context.Context, movieID string) (*plexResponse, error) {
	if s.catalog == nil {
		return nil, errors.New("Plex is not configured")
	}
	path, err := s.store.MoviePoster(ctx, movieID)
	if err != nil {
		return nil, err
	}
	response, err := s.catalog.Poster(ctx, path)
	if err != nil {
		return nil, err
	}
	return &plexResponse{Body: response.Body, Header: response.Header}, nil
}

type plexResponse struct {
	Body   io.ReadCloser
	Header http.Header
}

// Subscribe registers a room change listener and returns its cancellation function.
func (s *Service) Subscribe(code string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	if s.events[code] == nil {
		s.events[code] = make(map[chan struct{}]struct{})
	}
	s.events[code][ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.events[code], ch)
		if len(s.events[code]) == 0 {
			delete(s.events, code)
		}
		s.mu.Unlock()
	}
}

// Notify signals all listeners that a room has changed.
func (s *Service) Notify(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.events[code] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// canonicalGenres validates participant genres and returns their canonical room spelling.
func canonicalGenres(selected, available []string) ([]string, error) {
	canonical := make(map[string]string, len(available))
	for _, genre := range available {
		trimmed := strings.TrimSpace(genre)
		if trimmed != "" {
			canonical[strings.ToLower(trimmed)] = trimmed
		}
	}
	seen := make(map[string]struct{}, len(selected))
	result := make([]string, 0, len(selected))
	for _, genre := range selected {
		trimmed := strings.TrimSpace(genre)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		value, ok := canonical[key]
		if !ok {
			return nil, fmt.Errorf("genre %q is not available in this room", trimmed)
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

// collectGenres adds non-empty genres to a case-insensitive canonical set.
func collectGenres(target map[string]string, genres []string) {
	for _, genre := range genres {
		trimmed := strings.TrimSpace(genre)
		if trimmed != "" {
			target[strings.ToLower(trimmed)] = trimmed
		}
	}
}

// genreValues returns sorted canonical genre values from a normalized set.
func genreValues(genres map[string]string) []string {
	values := make([]string, 0, len(genres))
	for _, genre := range genres {
		values = append(values, genre)
	}
	sort.Strings(values)
	return values
}

// cleanName normalizes and limits a participant name.
func cleanName(name string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	runes := []rune(name)
	if len(runes) > 30 {
		name = string(runes[:30])
	}
	return name
}

// roomCode creates a short random room identifier.
func roomCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	for i := range bytes {
		bytes[i] = alphabet[int(bytes[i])%len(alphabet)]
	}
	return string(bytes), nil
}

// credentials creates a participant identifier and authentication token.
func credentials() (id, token, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	id = hex.EncodeToString(hash[:12])
	tokenHash = hex.EncodeToString(hash[:])
	return
}

// hashToken returns the stored digest of a participant token.
func hashToken(token string) string {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "invalid"
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// SortLibraries orders libraries by media type and title.
func SortLibraries(libraries []plex.Library) {
	sort.Slice(libraries, func(i, j int) bool {
		if libraries[i].Type != libraries[j].Type {
			return libraries[i].Type < libraries[j].Type
		}
		return libraries[i].Title < libraries[j].Title
	})
}
