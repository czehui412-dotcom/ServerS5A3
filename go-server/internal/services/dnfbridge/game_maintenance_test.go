package dnfbridge

import (
	"net"
	"sync/atomic"
	"testing"

	"longheng.io/server/internal/modules/dnf/adminapi"
)

type maintenanceConn struct {
	*bufferConn
	closed atomic.Bool
}

func (c *maintenanceConn) Close() error {
	c.closed.Store(true)
	return c.bufferConn.Close()
}

func TestMaintenanceEnterKicksAllLiveAndOrphanSessions(t *testing.T) {
	s := &Service{
		liveGameSessions: map[string]*gameSession{},
		gameSessions:     map[uint16]*gameSession{},
		conns:            map[net.Conn]struct{}{},
	}

	// Two live sessions (one reachable via liveGameSessions, one only via
	// gameSessions) plus one orphan conn with no session at all.
	liveConn := &maintenanceConn{bufferConn: &bufferConn{}}
	dirConn := &maintenanceConn{bufferConn: &bufferConn{}}
	orphan := &maintenanceConn{bufferConn: &bufferConn{}}

	liveSession := &gameSession{conn: liveConn}
	dirSession := &gameSession{conn: dirConn}
	s.liveGameSessions["c1"] = liveSession
	s.gameSessions[11] = dirSession
	s.conns[liveConn] = struct{}{}
	s.conns[dirConn] = struct{}{}
	s.conns[orphan] = struct{}{}

	st, err := s.applyMaintenance(&adminapi.MaintenanceChange{Op: "enter", Message: "更新中"})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled {
		t.Fatal("maintenance not enabled")
	}
	if st.Kicked != 2 {
		t.Fatalf("kicked=%d, want 2 sessions", st.Kicked)
	}
	if !liveConn.closed.Load() || !dirConn.closed.Load() {
		t.Fatal("live sessions were not closed")
	}
	if !orphan.closed.Load() {
		t.Fatal("orphan conn was not closed")
	}
	if !s.maintenanceEnabled() {
		t.Fatal("login path should reject while maintenance active")
	}
}

func TestMaintenanceLeaveReallowsLogin(t *testing.T) {
	s := &Service{}
	if _, err := s.applyMaintenance(&adminapi.MaintenanceChange{Op: "enter"}); err != nil {
		t.Fatal(err)
	}
	if !s.maintenanceEnabled() {
		t.Fatal("expected active")
	}
	st, err := s.applyMaintenance(&adminapi.MaintenanceChange{Op: "leave"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || s.maintenanceEnabled() {
		t.Fatal("maintenance not cleared after leave")
	}
}

func TestMaintenanceStatusReadonly(t *testing.T) {
	s := &Service{}
	st, err := s.applyMaintenance(&adminapi.MaintenanceChange{Op: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled {
		t.Fatal("fresh service should not be in maintenance")
	}
}
