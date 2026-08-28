package room

import (
	"context"
	"strings"
)

// RoomsForIdentity returns active room memberships associated with a persistent browser identity.
func (s *Service) RoomsForIdentity(
	ctx context.Context,
	identityToken string,
) ([]Membership, error) {
	if strings.TrimSpace(identityToken) == "" {
		return nil, InvalidInput("browser identity is required")
	}
	return s.memberships.RoomMemberships(ctx, hashToken(identityToken))
}

// ResumeIdentity restores the participant session associated with a browser identity and room.
func (s *Service) ResumeIdentity(ctx context.Context, identityToken, code string) (Session, error) {
	if strings.TrimSpace(identityToken) == "" {
		return Session{}, InvalidInput("browser identity is required")
	}

	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 6 {
		return Session{}, InvalidInput("a six-character room code is required")
	}

	session, err := s.memberships.RoomMembershipSession(ctx, hashToken(identityToken), code)
	if err != nil {
		return Session{}, err
	}

	return Session(session), nil
}

// membershipCredential builds a persisted browser membership for a participant token.
func membershipCredential(identityToken, participantToken string) MembershipCredential {
	return MembershipCredential{
		IdentityHash: hashToken(identityToken),
		SessionToken: participantToken,
	}
}
