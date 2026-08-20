package room

import (
	"context"
	"errors"
	"strings"

	"github.com/gi8lino/screendeck/internal/store"
)

// RoomsForIdentity returns active room memberships associated with a persistent browser identity.
func (s *Service) RoomsForIdentity(ctx context.Context, identityToken string) ([]store.RoomMembership, error) {
	if strings.TrimSpace(identityToken) == "" {
		return nil, errors.New("browser identity is required")
	}
	return s.store.RoomMemberships(ctx, hashToken(identityToken))
}

// ResumeIdentity restores the participant session associated with a browser identity and room.
func (s *Service) ResumeIdentity(ctx context.Context, identityToken, code string) (Session, error) {
	if strings.TrimSpace(identityToken) == "" {
		return Session{}, errors.New("browser identity is required")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 6 {
		return Session{}, errors.New("a six-character room code is required")
	}
	session, err := s.store.RoomMembershipSession(ctx, hashToken(identityToken), code)
	if err != nil {
		return Session{}, err
	}
	return Session{Code: session.Code, Token: session.Token}, nil
}

// ClaimIdentity associates an existing participant session with a persistent browser identity.
func (s *Service) ClaimIdentity(ctx context.Context, identityToken, code, participantToken string) error {
	if strings.TrimSpace(identityToken) == "" {
		return errors.New("browser identity is required")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 6 {
		return errors.New("a six-character room code is required")
	}
	participant, err := s.store.ParticipantByToken(ctx, code, hashToken(participantToken))
	if err != nil {
		return err
	}
	return s.store.SaveRoomMembership(
		ctx,
		code,
		participant.ID,
		store.RoomMembershipCredential{
			IdentityHash: hashToken(identityToken),
			SessionToken: participantToken,
		},
	)
}

// membershipCredentials builds an optional persisted browser membership for a participant token.
func membershipCredentials(identityToken, participantToken string) []store.RoomMembershipCredential {
	if strings.TrimSpace(identityToken) == "" {
		return nil
	}
	return []store.RoomMembershipCredential{{
		IdentityHash: hashToken(identityToken),
		SessionToken: participantToken,
	}}
}
