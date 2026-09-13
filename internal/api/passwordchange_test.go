package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/holiaokho/holiaokho/internal/auth"
)

// The lock has to live on the server: the reason an account owes a password
// change is that its password may already be known to people who would happily
// skip the UI and call the API directly.
func TestPasswordChangeOnly(t *testing.T) {
	a := &API{}
	reached := false
	h := a.passwordChangeOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(200)
	}))

	cases := []struct {
		name       string
		principal  *auth.Principal
		method     string
		path       string
		wantStatus int
	}{
		{"未登入不受影響", nil, http.MethodGet, "/repositories", 200},
		{"一般使用者暢通", &auth.Principal{Username: "alice"}, http.MethodGet, "/repositories", 200},

		{"欠改密碼者被擋", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodGet, "/repositories", 403},
		{"連建立使用者也被擋", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodPost, "/users", 403},
		{"改別人密碼也被擋", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodPut, "/users/bob/password", 403},
		{"不能先發 token 再繞過", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodPost, "/me/tokens", 403},

		{"改自己密碼放行", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodPut, "/me/password", 200},
		{"查看自己身分放行", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodGet, "/session", 200},
		{"登出放行", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodDelete, "/session", 200},
		{"尾斜線同樣放行", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodPut, "/me/password/", 200},

		// The allow-list is matched on method too, so a path that is open for
		// one verb does not become an open door for every other verb.
		{"白名單路徑的其他動詞仍被擋", &auth.Principal{Username: "admin", MustChangePassword: true}, http.MethodDelete, "/me/password", 403},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached = false
			r := httptest.NewRequest(c.method, c.path, nil)
			if c.principal != nil {
				r = r.WithContext(auth.WithPrincipal(r.Context(), c.principal))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != c.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, c.wantStatus)
			}
			if want := c.wantStatus == 200; reached != want {
				t.Errorf("handler reached = %v, want %v", reached, want)
			}
		})
	}
}
