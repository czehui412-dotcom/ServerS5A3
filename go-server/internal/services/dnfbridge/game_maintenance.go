package dnfbridge

import (
	"errors"
	"net"
	"sync"
	"time"

	"longheng.io/server/internal/modules/dnf/adminapi"
)

// maintenanceStatus returns a copy of the current maintenance flag. It never
// blocks gameplay: the maintenance lock is separate from the session lock.
func (s *Service) maintenanceStatus() *adminapi.MaintenanceStatus {
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	out := &adminapi.MaintenanceStatus{
		Enabled:   s.maintenanceActive,
		Message:   s.maintenanceMsg,
		Kicked:    s.maintenanceKicked,
	}
	if !s.maintenanceSince.IsZero() {
		out.Since = s.maintenanceSince.UTC().Format(time.RFC3339)
	}
	return out
}

// maintenanceEnabled is checked on the login path so kicked players cannot
// immediately reconnect while maintenance is in effect.
func (s *Service) maintenanceEnabled() bool {
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	return s.maintenanceActive
}

// applyMaintenance handles the adminapi "maintenance" action.
//   - enter: mark maintenance active (rejects new logins), then close every
//     live game transport. Closing a socket follows the existing disconnect
//     path that already flushes character state, the same path used by account
//     bans and password resets.
//   - leave: clear the flag so logins are accepted again.
//   - status: read-only snapshot.
func (s *Service) applyMaintenance(c *adminapi.MaintenanceChange) (*adminapi.MaintenanceStatus, error) {
	if c == nil || c.Op == "" {
		return s.maintenanceStatus(), errors.New("maintenance op required")
	}
	switch c.Op {
	case "status":
		return s.maintenanceStatus(), nil
	case "leave":
		s.maintenanceMu.Lock()
		s.maintenanceActive = false
		s.maintenanceMsg = ""
		s.maintenanceSince = time.Time{}
		s.maintenanceMu.Unlock()
		return s.maintenanceStatus(), nil
	case "enter":
		// 1) Set the flag first so no reconnect slips in during the kick sweep.
		s.maintenanceMu.Lock()
		s.maintenanceActive = true
		s.maintenanceMsg = c.Message
		s.maintenanceSince = time.Now().UTC()
		s.maintenanceMu.Unlock()

		// 2) Snapshot every live game transport under the session lock, then
		//    close outside it. liveGameSessions covers sockets not visible in
		//    the per-character gameSessions directory (area handover), and
		//    conns is a safety net for authenticated-but-not-yet-bound sockets.
		s.mu.Lock()
		seen := make(map[*gameSession]struct{}, len(s.liveGameSessions)+len(s.gameSessions))
		sessions := make([]*gameSession, 0, len(s.liveGameSessions)+len(s.gameSessions))
		for _, session := range s.liveGameSessions {
			if session != nil {
				if _, ok := seen[session]; !ok {
					seen[session] = struct{}{}
					sessions = append(sessions, session)
				}
			}
		}
		for _, session := range s.gameSessions {
			if session != nil {
				if _, ok := seen[session]; !ok {
					seen[session] = struct{}{}
					sessions = append(sessions, session)
				}
			}
		}
		// Raw sockets that have not bound to a session yet (login in flight).
		orphanConns := make([]net.Conn, 0, len(s.conns))
		for conn := range s.conns {
			orphanConns = append(orphanConns, conn)
		}
		s.mu.Unlock()

		kicked := len(sessions)
		var wg sync.WaitGroup
		for _, session := range sessions {
			wg.Add(1)
			go func(p *gameSession) {
				defer wg.Done()
				p.closeGameTransport()
			}(session)
		}
		for _, conn := range orphanConns {
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				_ = c.Close()
			}(conn)
		}
		wg.Wait()

		s.maintenanceMu.Lock()
		s.maintenanceKicked = kicked
		s.maintenanceMu.Unlock()
		return s.maintenanceStatus(), nil
	default:
		return s.maintenanceStatus(), errors.New("unknown maintenance op")
	}
}
