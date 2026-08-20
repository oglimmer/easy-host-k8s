package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/sessions"
)

func TestSafeNext(t *testing.T) {
	const slug = "my-page"
	tests := []struct {
		name string
		next string
		want string
	}{
		{"the content root", "/s/my-page", "/s/my-page"},
		{"a sub-resource", "/s/my-page/css/site.css", "/s/my-page/css/site.css"},
		{"a nested path", "/s/my-page/a/b/c.html", "/s/my-page/a/b/c.html"},
		{"query string kept", "/s/my-page/index.html?x=1", "/s/my-page/index.html?x=1"},

		{"empty", "", "/s/my-page"},
		{"absolute URL", "https://evil.example.com", "/s/my-page"},
		{"scheme-relative URL", "//evil.example.com", "/s/my-page"},
		{"backslash host", "/s/my-page/\\evil.example.com", "/s/my-page"},
		{"embedded double slash", "/s/my-page//evil.example.com", "/s/my-page"},
		{"traversal out of the content", "/s/my-page/../../dashboard", "/s/my-page"},
		{"another slug", "/s/other-page", "/s/my-page"},
		{"a slug this one is a prefix of", "/s/my-page-2", "/s/my-page"},
		{"elsewhere in the app", "/dashboard", "/s/my-page"},
		{"header injection", "/s/my-page\r\nSet-Cookie: x=1", "/s/my-page"},
	}
	for _, tt := range tests {
		if got := safeNext(slug, tt.next); got != tt.want {
			t.Errorf("%s: safeNext(%q, %q) = %q, want %q", tt.name, slug, tt.next, got, tt.want)
		}
	}
}

func TestContentPath(t *testing.T) {
	if got := contentPath("my-page"); got != "/s/my-page" {
		t.Errorf("contentPath = %q", got)
	}
}

// TestRenderUnlock exercises the real template set, so a broken unlock.html
// fails here rather than in front of a visitor.
func TestRenderUnlock(t *testing.T) {
	h := testWebHandler(t)
	w := httptest.NewRecorder()

	h.RenderUnlock(w, 401, UnlockPromptData{Slug: "my-page", Next: "/s/my-page/deep.html", Error: "Wrong passphrase."})

	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := w.Body.String()
	for _, want := range []string{
		`action="/unlock/my-page"`,
		`name="next" value="/s/my-page/deep.html"`,
		`type="password"`,
		`name="passphrase"`,
		"Wrong passphrase.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page is missing %q", want)
		}
	}
}

// TestRenderUnlockEscapes checks that a slug or message can never break out of
// the markup — the prompt is served on a public, unauthenticated path.
func TestRenderUnlockEscapes(t *testing.T) {
	h := testWebHandler(t)
	w := httptest.NewRecorder()

	h.RenderUnlock(w, 401, UnlockPromptData{
		Slug:  `"><script>alert(1)</script>`,
		Next:  `" onload="alert(2)`,
		Error: `<img src=x onerror=alert(3)>`,
	})

	body := w.Body.String()
	for _, unwanted := range []string{"<script>alert(1)", `onload="alert(2)`, "<img src=x onerror"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("unescaped payload %q made it into the page", unwanted)
		}
	}
}

func testWebHandler(t *testing.T) *WebHandler {
	t.Helper()
	store := sessions.NewCookieStore([]byte("test-session-secret-32-bytes-ok!"))
	// The unlock prompt needs neither the content service nor the user store.
	return NewWebHandler(nil, nil, store, "../../templates", 0, false, nil, "", "http://localhost")
}
