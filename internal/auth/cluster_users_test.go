package auth

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Two replicas over one shared store: directory changes made through one are
// what the other evaluates, and a change prepared against a stale revision is
// re-evaluated (not blindly applied) after a CAS conflict.
func TestClusterDirectorySharedThroughCAS(t *testing.T) {
	store := &memoryClusterAuthStore{}
	nodeA := newTestClusterAuth(t, store)
	nodeB := newTestClusterAuth(t, store)

	owner, _, err := nodeA.Login(dirAdminEmail, "TestPass1234!")
	if err != nil {
		t.Fatal(err)
	}
	second, err := nodeA.CreateUser(owner, api.CreateUserRequest{Email: "owner2@example.com", Password: dirUserPass, Role: core.RoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	// Node B sees the account and can sign it in.
	if members, err := nodeB.ListMembers("default"); err != nil || len(members) != 2 {
		t.Fatalf("node B members = %d, %v", len(members), err)
	}
	user2, tok2, err := nodeB.Login("owner2@example.com", dirUserPass)
	if err != nil || user2.Role != "owner" {
		t.Fatalf("node B login of a node A account: %+v %v", user2, err)
	}

	// Race: while node A demotes the configured admin, node B demotes the
	// other owner. Each alone is legal; together they would leave no owner.
	// Node A's CAS conflicts, it re-evaluates against node B's commit, and
	// the last-owner rule refuses.
	admin := core.RoleAdmin
	secondActor := &api.User{ID: second.ID, Email: second.Email, Workspace: "default"}
	store.conflictOnce = func() {
		if _, err := nodeB.UpdateMember(secondActor, second.ID, api.MemberUpdate{Role: &admin}); err != nil {
			t.Errorf("node B demotion: %v", err)
		}
	}
	_, err = nodeA.UpdateMember(owner, owner.ID, api.MemberUpdate{Role: &admin})
	if errKind(err) != "conflict" || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("racing demotions left the workspace ownerless: %v", err)
	}
	members, _ := nodeA.ListMembers("default")
	owners := 0
	for _, m := range members {
		if m.Role == core.RoleOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("owners after race = %d, want 1", owners)
	}
	// Node B's demotion revoked owner2's session cluster-wide.
	if _, err := nodeA.Authenticate(tok2); err == nil {
		t.Fatal("a downgraded session still authenticates on the other replica")
	}

	// Invitations: issued on one replica, redeemed once on the other.
	_, token, err := nodeA.CreateInvitation(owner, "invitee@example.com", core.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodeB.AcceptInvitation(token, "Invitee", dirUserPass); err != nil {
		t.Fatalf("accept on node B: %v", err)
	}
	if _, err := nodeA.AcceptInvitation(token, "Invitee", dirUserPass); errKind(err) != "validation" {
		t.Fatalf("token redeemed twice across replicas: %v", err)
	}

	// A refused change commits nothing.
	before := store.state.Revision
	if _, err := nodeA.CreateUser(owner, api.CreateUserRequest{Email: "bad", Password: dirUserPass}); err == nil {
		t.Fatal("invalid email accepted")
	}
	if store.state.Revision != before {
		t.Fatal("a refused change was committed")
	}
}

func TestClusterDirectoryOversizeIsRefusedClearly(t *testing.T) {
	big := sessionData{Users: map[string]*api.User{}, Accounts: map[string]*accountRecord{}}
	for i := 0; i < 1200; i++ {
		id, _ := generateID()
		big.Users[id] = &api.User{ID: id, Email: id + "@example.com", Name: strings.Repeat("n", 60)}
		big.Accounts[id] = &accountRecord{PasswordHash: strings.Repeat("h", 60), Memberships: map[string]core.MemberRole{"default": core.RoleViewer}}
	}
	raw, _ := json.Marshal(big)
	err := validateClusterAuthData(raw)
	if err == nil || !strings.Contains(err.Error(), "replicated transaction budget") || !strings.Contains(err.Error(), "accounts") {
		t.Fatalf("oversized directory: %v", err)
	}
}
