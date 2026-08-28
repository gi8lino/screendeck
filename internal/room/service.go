package room

import (
	"sync"
	"time"

	"github.com/gi8lino/screendeck/internal/media"
	"golang.org/x/sync/singleflight"
)

const (
	maxRoundSize             = 50000
	libraryCacheTTL          = 5 * time.Minute
	minimumRoomLifetimeHours = 6
	maximumRoomLifetimeHours = 7 * 24
)

// Service orchestrates room behavior, catalog access, caching, and live notifications.
type Service struct {
	// rooms persists room lifecycle and gameplay state.
	rooms RoomStore
	// catalogStore persists cached provider-neutral metadata.
	catalogStore CatalogStore
	// memberships restores persistent browser room associations.
	memberships MembershipStore
	// catalog provides media metadata and poster access.
	catalog media.Catalog
	// roomTTL controls expiration for newly created rooms.
	roomTTL time.Duration
	// excludedLibraries contains normalized media library titles or keys hidden from room creation.
	excludedLibraries map[string]struct{}
	// mu protects mutable in-memory state.
	mu sync.Mutex
	// events contains live room subscriber channels keyed by room code.
	events map[string]map[chan struct{}]struct{}
	// cache stores recently loaded library contents.
	cache map[string]cacheEntry
	// libraryLoads coalesces concurrent cache misses for the same library.
	libraryLoads singleflight.Group
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
	// SamplingRecentlyAdded orders eligible titles by provider added-at time before applying the round-size limit.
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

// cacheEntry stores a recently loaded media library result.
type cacheEntry struct {
	// items contains cached media items.
	items []media.Item
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
func NewService(
	database Store,
	catalog media.Catalog,
	roomTTL time.Duration,
	excludedLibraries []string,
) *Service {
	return &Service{
		rooms:             database,
		catalogStore:      database,
		memberships:       database,
		catalog:           catalog,
		roomTTL:           roomTTL,
		excludedLibraries: normalizeLibraryExclusions(excludedLibraries),
		events:            make(map[string]map[chan struct{}]struct{}),
		cache:             make(map[string]cacheEntry),
	}
}
