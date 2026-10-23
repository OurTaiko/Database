package httpapi

import (
	"context"
	"time"
)

// publicProfiles names users for public pages. Catalogs stay available during an
// identity-provider outage (or without SSO in development): unknown users then
// show a placeholder. Account checks never use this fallback.
func (s *Server) publicProfiles(ctx context.Context, ids []string) map[string]PublicProfile {
	result := map[string]PublicProfile{}
	unique := []string{}
	for _, id := range ids {
		if _, ok := result[id]; !ok {
			unique = append(unique, id)
			result[id] = PublicProfile{Nickname: "未知用户"}
		}
	}
	if s.Config.SSO == nil || len(unique) == 0 {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if names, e := s.Config.SSO.Profiles(ctx, unique); e == nil {
		for id, name := range names {
			result[id] = name
		}
	}
	return result
}

// avatarOf is the signed-in user's avatar, or none without SSO.
func (s *Server) avatarOf(u User) string {
	if s.Config.SSO == nil {
		return ""
	}
	return s.Config.SSO.avatarURL(u)
}
