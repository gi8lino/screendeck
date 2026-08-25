package maintenance

import (
	"context"
	"log/slog"
	"time"
)

// expiredRoomStore defines the persistence operation required by room cleanup.
type expiredRoomStore interface {
	DeleteExpired(context.Context) error
}

// RunRoomCleanup periodically removes expired rooms until shutdown.
func RunRoomCleanup(
	ctx context.Context,
	store expiredRoomStore,
	roomCleanupInterval time.Duration,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(roomCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.DeleteExpired(ctx); err != nil {
				logger.Error("delete expired rooms",
					"event", "delete_expired_rooms_failed",
					"error", err,
				)
			}
		}
	}
}
