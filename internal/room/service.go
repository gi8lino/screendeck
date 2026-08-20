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

// Service orchestrates room behavior, catalog access, caching, and live notifications.
type Service struct {
	// store persists rooms and catalog metadata.
	store *store.Store
	// catalog provides media metadata and poster access.
	catalog Catalog
	// roomTTL controls expiration for newly created rooms.
	roomTTL time.Duration
	// mu protects mutable in-memory state.
	mu sync.Mutex
	// events contains live room subscriber channels keyed by room code.
	events map[string]map[chan struct{}]struct{}
	// cache stores recently loaded library contents.
	cache map[string]cacheEntry
}

// Catalog defines the Plex catalog operations required by the room service.
type Catalog interface {
	Libraries(context.Context) ([]plex.Library, error)
	Items(context.Context, plex.Library) ([]plex.Item, error)
	Poster(context.Context, string) (*http.Response, error)
}

// Filters contains room-wide catalog filtering criteria.
type Filters struct {
	// Genres contains selected genre names.
	Genres []string `json:"genres"`
	// YearFrom is the inclusive lower release-year bound.
	YearFrom int `json:"yearFrom"`
	// YearTo is the inclusive upper release-year bound.
	YearTo int `json:"yearTo"`
	// MaxDurationMinutes limits movie duration when non-zero.
	MaxDurationMinutes int `json:"maxDurationMinutes"`
	// UnwatchedOnly restricts the room to unwatched items.
	UnwatchedOnly bool `json:"unwatchedOnly"`
}

// GenreMode controls how a participant combines selected personal genres.
type GenreMode string

const (
	// GenreModeAny accepts titles matching at least one selected personal genre.
	GenreModeAny GenreMode = "any"
	// GenreModeAll accepts only titles matching every selected personal genre.
	GenreModeAll GenreMode = "all"
)

// SamplingStrategy controls how the first-round candidate pool is ordered.
type SamplingStrategy string

const (
	// SamplingRandom shuffles all eligible titles before applying the round-size limit.
	SamplingRandom SamplingStrategy = "random"
	// SamplingHighestRated orders eligible titles by rating before applying the round-size limit.
	SamplingHighestRated SamplingStrategy = "highest_rated"
	// SamplingRecentlyAdded orders eligible titles by Plex added-at time before applying the round-size limit.
	SamplingRecentlyAdded SamplingStrategy = "recently_added"
	// SamplingRandomUnwatched shuffles only unwatched eligible titles before applying the round-size limit.
	SamplingRandomUnwatched SamplingStrategy = "random_unwatched"
)

// CatalogOptions contains filter values derived from selected libraries.
type CatalogOptions struct {
	// Genres contains selected genre names.
	Genres []string `json:"genres"`
	// MinYear is the earliest release year in the selected libraries.
	MinYear int `json:"minYear"`
	// MaxYear is the latest release year in the selected libraries.
	MaxYear int `json:"maxYear"`
}

// MoreTitlesResult reports the result of expanding the first-round deck.
type MoreTitlesResult struct {
	// Added is the number of titles activated by an expansion.
	Added int `json:"added"`
	// Remaining is the number of unused titles still available.
	Remaining int `json:"remaining"`
}

// RoundResult reports next-round readiness and advancement state.
type RoundResult struct {
	// Round identifies the room round the readiness update applies to.
	Round int `json:"round"`
	// Titles is the number of titles in the resulting round.
	Titles int `json:"titles"`
	// Ready is the number of participants ready to advance.
	Ready int `json:"ready"`
	// Required is the number of active participants whose readiness is required.
	Required int `json:"required"`
	// Advanced reports whether the room advanced during the operation.
	Advanced bool `json:"advanced"`
}

// cacheEntry stores a recently loaded Plex library result.
type cacheEntry struct {
	// items contains cached Plex items.
	items []plex.Item
	// fetchedAt records when the cache entry was populated.
	fetchedAt time.Time
}

// Session contains the room code and participant authentication token.
type Session struct {
	// Code is the six-character room identifier.
	Code string `json:"code"`
	// Token authenticates the participant to the room.
	Token string `json:"token"`
}

// NewService creates a room service backed by the supplied catalog and store.
func NewService(database *store.Store, catalog Catalog, roomTTL time.Duration) *Service {
	return &Service{store: database, catalog: catalog, roomTTL: roomTTL, events: make(map[string]map[chan struct{}]struct{}), cache: make(map[string]cacheEntry)}
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
func (s *Service) Create(ctx context.Context, name string, libraryKeys []string, filters Filters, genres []string, genreMode GenreMode, sampling SamplingStrategy, roundSize int) (Session, error) {
	name = cleanName(name)
	if name == "" {
		return Session{}, errors.New("name is required")
	}
	if len(libraryKeys) == 0 {
		return Session{}, errors.New("select at least one library")
	}
	if err := validateFilters(filters); err != nil {
		return Session{}, err
	}
	if roundSize < 0 || roundSize > 50000 {
		return Session{}, errors.New("round size must be between 0 and 50000 titles")
	}
	genreMode = normalizeGenreMode(genreMode)
	if genreMode == "" {
		return Session{}, errors.New("genre mode must be any or all")
	}
	if sampling == "" {
		sampling = SamplingRandom
	}
	if !validSamplingStrategy(sampling) {
		return Session{}, errors.New("invalid first-round selection strategy")
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
	eligible := make([]plex.Item, 0, len(items))
	for _, item := range items {
		if !matchesFilters(item, filters, genreSet) || seen[item.RatingKey] {
			continue
		}
		seen[item.RatingKey] = true
		eligible = append(eligible, item)
	}
	if len(eligible) == 0 {
		return Session{}, errors.New("the selected libraries contain no matching titles")
	}
	pool, err := orderInitialItems(eligible, sampling)
	if err != nil {
		return Session{}, err
	}
	availableGenreSet := make(map[string]string)
	for _, item := range pool {
		collectGenres(availableGenreSet, item.Genres)
	}
	participantGenres, err := canonicalGenres(genres, genreValues(availableGenreSet))
	if err != nil {
		return Session{}, err
	}
	selected := pool
	if roundSize > 0 && len(selected) > roundSize {
		selected = selected[:roundSize]
	}
	itemIDs := itemRatingKeys(selected)
	poolIDs := itemRatingKeys(pool)
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
		store.Participant{ID: participantID, Name: name, Genres: participantGenres, GenreMode: string(genreMode)},
		tokenHash,
		itemIDs,
		poolIDs,
	)
	if err != nil {
		return Session{}, err
	}
	return Session{Code: code, Token: token}, nil
}

// validateFilters verifies room-wide catalog filter bounds.
func validateFilters(filters Filters) error {
	if filters.YearFrom < 0 || filters.YearTo < 0 || filters.MaxDurationMinutes < 0 {
		return errors.New("invalid catalog filters")
	}
	if filters.YearFrom > 0 && filters.YearTo > 0 && filters.YearFrom > filters.YearTo {
		return errors.New("invalid catalog filters")
	}
	return nil
}

// validSamplingStrategy reports whether a first-round selection strategy is supported.
func validSamplingStrategy(strategy SamplingStrategy) bool {
	switch strategy {
	case SamplingRandom, SamplingHighestRated, SamplingRecentlyAdded, SamplingRandomUnwatched:
		return true
	default:
		return false
	}
}

// orderInitialItems orders and filters the room's original eligible pool.
func orderInitialItems(items []plex.Item, strategy SamplingStrategy) ([]plex.Item, error) {
	selected := append([]plex.Item(nil), items...)
	switch strategy {
	case SamplingRandom:
		mathrand.Shuffle(len(selected), func(i, j int) { selected[i], selected[j] = selected[j], selected[i] })
	case SamplingHighestRated:
		sort.SliceStable(selected, func(i, j int) bool {
			if selected[i].Rating != selected[j].Rating {
				return selected[i].Rating > selected[j].Rating
			}
			return selected[i].Title < selected[j].Title
		})
	case SamplingRecentlyAdded:
		sort.SliceStable(selected, func(i, j int) bool {
			if selected[i].AddedAt != selected[j].AddedAt {
				return selected[i].AddedAt > selected[j].AddedAt
			}
			return selected[i].Title < selected[j].Title
		})
	case SamplingRandomUnwatched:
		unwatched := selected[:0]
		for _, item := range selected {
			if !item.Viewed {
				unwatched = append(unwatched, item)
			}
		}
		selected = unwatched
		if len(selected) == 0 {
			return nil, errors.New("no unwatched titles match the selected room filters")
		}
		mathrand.Shuffle(len(selected), func(i, j int) { selected[i], selected[j] = selected[j], selected[i] })
	default:
		return nil, errors.New("invalid first-round selection strategy")
	}
	return selected, nil
}

// selectInitialItems returns the first limited portion of the ordered eligible pool.
func selectInitialItems(items []plex.Item, strategy SamplingStrategy, limit int) ([]plex.Item, error) {
	selected, err := orderInitialItems(items, strategy)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(selected) > limit {
		selected = selected[:limit]
	}
	return selected, nil
}

// itemRatingKeys converts media items to their Plex rating keys in order.
func itemRatingKeys(items []plex.Item) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.RatingKey)
	}
	return keys
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
func (s *Service) Join(ctx context.Context, code, name string, genres []string, genreMode GenreMode) (Session, error) {
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
	genreMode = normalizeGenreMode(genreMode)
	if genreMode == "" {
		return Session{}, errors.New("genre mode must be any or all")
	}
	participantID, token, tokenHash, err := credentials()
	if err != nil {
		return Session{}, err
	}
	if err := s.store.JoinRoom(ctx, code, store.Participant{ID: participantID, Name: name, Genres: participantGenres, GenreMode: string(genreMode)}, tokenHash); err != nil {
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
func (s *Service) Vote(ctx context.Context, code, token, itemID string, liked bool) (bool, error) {
	code = strings.ToUpper(code)
	participant, err := s.store.ParticipantByToken(ctx, code, hashToken(token))
	if err != nil {
		return false, err
	}
	matched, err := s.store.Vote(ctx, code, participant.ID, itemID, liked)
	if err == nil {
		s.Notify(code)
	}
	return matched, err
}

// AddMoreTitles expands the first round from its unused original eligible pool.
func (s *Service) AddMoreTitles(ctx context.Context, code, token string, count int) (MoreTitlesResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	participant, err := s.store.ParticipantByToken(ctx, code, hashToken(token))
	if err != nil {
		return MoreTitlesResult{}, err
	}
	added, remaining, err := s.store.AddMoreTitles(ctx, code, participant.ID, count)
	if err != nil {
		return MoreTitlesResult{}, err
	}
	s.Notify(code)
	return MoreTitlesResult{Added: added, Remaining: remaining}, nil
}

// SetNextRoundReady records whether a participant agrees to narrow the deck to current matches.
func (s *Service) SetNextRoundReady(ctx context.Context, code, token string, expectedRound int, ready bool) (RoundResult, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	participant, err := s.store.ParticipantByToken(ctx, code, hashToken(token))
	if err != nil {
		return RoundResult{}, err
	}
	round, titles, readyCount, required, advanced, err := s.store.SetRoundReady(ctx, code, participant.ID, expectedRound, ready)
	if err != nil {
		return RoundResult{}, err
	}
	s.Notify(code)
	return RoundResult{Round: round, Titles: titles, Ready: readyCount, Required: required, Advanced: advanced}, nil
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

// RemoveParticipant lets the current room host remove another participant from the room.
func (s *Service) RemoveParticipant(ctx context.Context, code, token, participantID string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return errors.New("participant id is required")
	}
	if err := s.store.RemoveParticipant(ctx, code, hashToken(token), participantID); err != nil {
		return err
	}
	s.Notify(code)
	return nil
}

// Poster retrieves the poster associated with a stored media item.
func (s *Service) Poster(ctx context.Context, itemID string) (*plexResponse, error) {
	if s.catalog == nil {
		return nil, errors.New("Plex is not configured")
	}
	path, err := s.store.ItemPoster(ctx, itemID)
	if err != nil {
		return nil, err
	}
	response, err := s.catalog.Poster(ctx, path)
	if err != nil {
		return nil, err
	}
	return &plexResponse{Body: response.Body, Header: response.Header}, nil
}

// plexResponse exposes only the poster response fields required by handlers.
type plexResponse struct {
	// Body is the proxied poster response body.
	Body io.ReadCloser
	// Header contains poster response headers from Plex.
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

// normalizeGenreMode returns a supported personal genre matching mode.
func normalizeGenreMode(mode GenreMode) GenreMode {
	if mode == "" {
		return GenreModeAny
	}
	switch mode {
	case GenreModeAny, GenreModeAll:
		return mode
	default:
		return ""
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
