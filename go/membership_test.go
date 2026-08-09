package authsdk

import (
	"net/http"
	"testing"
)

func TestMembership_GetListBindUnbind(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/consumer-auth/me/membership":
			if r.Header.Get("Authorization") != "Bearer user-jwt" {
				t.Fatalf("auth=%q", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("application_service") != "memoo" {
				t.Fatalf("application_service=%q", r.URL.Query().Get("application_service"))
			}
			writeEnvelope(w, http.StatusOK, Membership{
				ID: 9, ApplicationService: "memoo", Status: "active", PermissionKeys: []string{"notes.read"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/consumer-auth/memberships/9/bundles":
			writeEnvelope(w, http.StatusOK, []MembershipBundle{{
				ID: 11, ApplicationPermissionBundleID: 3, BundleCode: "memoo.viewer", Permissions: []string{"notes.read"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/consumer-auth/memberships/9/bundles":
			writeEnvelope(w, http.StatusCreated, MembershipBundle{
				ID: 12, ApplicationPermissionBundleID: 4, BundleCode: "memoo.member",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/consumer-auth/memberships/9/bundles/4":
			writeEnvelope(w, http.StatusOK, MembershipBundle{ID: 12, ApplicationPermissionBundleID: 4})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))

	ctx := t.Context()
	m, err := client.GetMyMembership(ctx, "user-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 9 || m.PermissionKeys[0] != "notes.read" {
		t.Fatalf("%+v", m)
	}

	list, err := client.ListMembershipBundles(ctx, "user-jwt", 9)
	if err != nil || len(list) != 1 || list[0].BundleCode != "memoo.viewer" {
		t.Fatalf("%v %+v", err, list)
	}

	bound, err := client.BindMembershipBundle(ctx, "user-jwt", 9, "memoo.member")
	if err != nil || bound.BundleCode != "memoo.member" {
		t.Fatalf("%v %+v", err, bound)
	}

	unbound, err := client.UnbindMembershipBundle(ctx, "user-jwt", 9, 4)
	if err != nil || unbound.ID != 12 {
		t.Fatalf("%v %+v", err, unbound)
	}
}

func TestMembership_Validation(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("should not call API")
	}))
	ctx := t.Context()
	if _, err := client.GetMyMembership(ctx, ""); !IsValidation(err) {
		t.Fatalf("got %v", err)
	}
	if _, err := client.ListMembershipBundles(ctx, "tok", 0); !IsValidation(err) {
		t.Fatalf("got %v", err)
	}
	if _, err := client.BindMembershipBundle(ctx, "tok", 1, ""); !IsValidation(err) {
		t.Fatalf("got %v", err)
	}
	if _, err := client.UnbindMembershipBundle(ctx, "tok", 1, 0); !IsValidation(err) {
		t.Fatalf("got %v", err)
	}
}
