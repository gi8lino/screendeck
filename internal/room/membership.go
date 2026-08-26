package room

import (
	"context"
	"errors"
	"strings"
)

// RoomsForIdentity returns active room memberships associated with a persistent browser identity.
func (s *Service) RoomsForIdentity(
	ctx context.Context,
	identityToken string,
) ([]Membership, error) {
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

// membershipCredential builds a persisted browser membership for a participant token.
func membershipCredential(identityToken, participantToken string) MembershipCredential {
	return MembershipCredential{
		IdentityHash: hashToken(identityToken),
		SessionToken: participantToken,
	}
}
