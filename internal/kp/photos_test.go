package kp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var _ PhotoService = (*Unsplash)(nil)

func validTestPhoto() Photo {
	return Photo{
		ID: "abc_123-X", URL: "https://images.unsplash.com/photo-123?ixid=original%2Bvalue&ixlib=rb-4.0.3",
		Alt: "A forest", Photographer: "A Photographer", PhotographerURL: "https://unsplash.com/@photographer",
		PageURL:     "https://unsplash.com/photos/forest-abc_123-X",
		DownloadURL: "https://api.unsplash.com/photos/abc_123-X/download?ixid=original%2Bvalue",
		Width:       6000, Height: 4000,
	}
}

func testUnsplash(t *testing.T, handler http.HandlerFunc) *Unsplash {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u := NewUnsplash("explicit-test-key")
	u.endpoint = server.URL
	return u
}

func photoAPIResult(p Photo) map[string]any {
	return map[string]any{
		"id": p.ID, "width": p.Width, "height": p.Height, "alt_description": p.Alt,
		"urls":  map[string]string{"raw": p.URL, "regular": "https://invalid.example/ignored"},
		"links": map[string]string{"html": p.PageURL, "download_location": p.DownloadURL},
		"user":  map[string]any{"name": p.Photographer, "links": map[string]string{"html": p.PhotographerURL}},
	}
}

func TestUnsplashSearch(t *testing.T) {
	t.Setenv("UNSPLASH_ACCESS_KEY", "wrong-environment-key")
	excluded := validTestPhoto()
	invalid := validTestPhoto()
	invalid.URL = "https://images.unsplash.com.evil.example/photo"
	want := validTestPhoto()
	want.ID = "next"
	want.DownloadURL = "https://api.unsplash.com/photos/next/download?ixid=keep%2Bme"
	var calls atomic.Int32
	u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/search/photos" {
			t.Errorf("unexpected method/path: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Client-ID explicit-test-key" || r.Header.Get("Accept-Version") != "v1" {
			t.Error("incorrect API headers")
		}
		wantQuery := map[string][]string{"query": {"forest and lake"}, "orientation": {"landscape"}, "content_filter": {"high"}, "per_page": {"10"}, "page": {"1"}}
		if !reflect.DeepEqual(map[string][]string(r.URL.Query()), wantQuery) {
			t.Errorf("incorrect search parameters: %v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{photoAPIResult(excluded), photoAPIResult(invalid), photoAPIResult(want)}})
	})
	got, err := u.Search(context.Background(), "  forest and lake  ", excluded.ID)
	if err != nil || got != want {
		t.Fatalf("Search = %+v, %v; want %+v", got, err, want)
	}
	if calls.Load() != 1 {
		t.Fatal("Search must not track or fetch image bytes")
	}
}

func TestUnsplashSearchFallbackAndNoPhoto(t *testing.T) {
	for _, alt := range []string{"A description", ""} {
		t.Run("fallback-"+alt, func(t *testing.T) {
			p := validTestPhoto()
			p.Alt = ""
			item := photoAPIResult(p)
			item["alt_description"] = nil
			item["description"] = alt
			u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{item}})
			})
			got, err := u.Search(context.Background(), "forest", "")
			if err != nil || got.Alt != alt {
				t.Fatalf("fallback = %q, %v", got.Alt, err)
			}
		})
	}
	for _, body := range []string{`{"results":[]}`, `{"results":[{"id":"invalid"}]}`} {
		u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		if _, err := u.Search(context.Background(), "forest", ""); !errors.Is(err, ErrNoPhoto) {
			t.Fatalf("expected ErrNoPhoto, got %v", err)
		}
	}
}

func TestUnsplashInvalidQuery(t *testing.T) {
	var calls atomic.Int32
	u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, query := range []string{"", "  ", strings.Repeat("a", 161), strings.Repeat("word ", 13), "forest\n", "\tforest", "a\x00b", string([]byte{0xff}), "alice@example.com", "https://customer.example/project", "192.168.1.1 office"} {
		if _, err := u.Search(context.Background(), query, ""); err == nil {
			t.Errorf("accepted invalid query %q", query)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid queries reached API")
	}
}

func TestUnsplashPhotographerUsername(t *testing.T) {
	photo := validTestPhoto()
	result := photoAPIResult(photo)
	user := result["user"].(map[string]any)
	user["name"], user["username"] = "", "actual-photographer"
	u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{result}})
	})
	got, err := u.Search(context.Background(), "office desk", "")
	if err != nil || got.Photographer != "actual-photographer" {
		t.Fatalf("photographer credit missing: %+v, %v", got, err)
	}
}

func TestValidatePhoto(t *testing.T) {
	if err := ValidatePhoto(validTestPhoto()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Photo){
		"empty ID":                func(p *Photo) { p.ID = "" },
		"long ID":                 func(p *Photo) { p.ID = strings.Repeat("x", 101) },
		"slash ID":                func(p *Photo) { p.ID = "a/b" },
		"escaped ID":              func(p *Photo) { p.ID = "a%2Fb" },
		"unicode ID":              func(p *Photo) { p.ID = "\u00e9" },
		"zero width":              func(p *Photo) { p.Width = 0 },
		"negative height":         func(p *Photo) { p.Height = -1 },
		"large dimension":         func(p *Photo) { p.Width = 100001 },
		"long alt":                func(p *Photo) { p.Alt = strings.Repeat("a", 2001) },
		"long photographer":       func(p *Photo) { p.Photographer = strings.Repeat("a", 501) },
		"long URL":                func(p *Photo) { p.URL += strings.Repeat("a", 4096) },
		"page path":               func(p *Photo) { p.PageURL = "https://unsplash.com/not-a-photo" },
		"empty photographer path": func(p *Photo) { p.PhotographerURL = "https://unsplash.com/" },
		"download mismatched ID":  func(p *Photo) { p.DownloadURL = "https://api.unsplash.com/photos/other/download" },
		"download escaped slash":  func(p *Photo) { p.DownloadURL = "https://api.unsplash.com/photos/abc_123-X%2Fdownload" },
	} {
		t.Run(name, func(t *testing.T) {
			p := validTestPhoto()
			mutate(&p)
			if ValidatePhoto(p) == nil {
				t.Fatal("accepted unsafe photo")
			}
		})
	}
	for _, bad := range []string{
		"http://images.unsplash.com/photo", "https://images.unsplash.com:443/photo",
		"https://images.unsplash.com.evil.example/photo", "https://127.0.0.1/photo",
		"https://localhost/photo", "https://images.unsplash.com@evil.example/photo",
		"https://user@images.unsplash.com/photo", "https://images.unsplash.com/photo#fragment",
		"https://images.unsplash.com/photo#", "https://images.unsplash.com/photo?client_id=secret",
		"https://images.unsplash.com/photo?%61ccess_token=secret", "https://images.unsplash.com/photo?CLIENT_ID=secret",
		"https://images.unsplash.com/photo?bad=%zz", "https://images.unsplash.com/\nphoto",
	} {
		p := validTestPhoto()
		p.URL = bad
		if ValidatePhoto(p) == nil {
			t.Errorf("accepted unsafe image URL %q", bad)
		}
	}
	for _, field := range []string{"page", "photographer", "download"} {
		p := validTestPhoto()
		switch field {
		case "page":
			p.PageURL = "https://evil.example/photos/abc"
		case "photographer":
			p.PhotographerURL = "https://evil.example/@someone"
		case "download":
			p.DownloadURL = "https://evil.example/photos/abc_123-X/download"
		}
		if ValidatePhoto(p) == nil {
			t.Errorf("accepted unsafe %s host", field)
		}
	}
	p := validTestPhoto()
	p.Alt = ""
	p.URL = "https://images.unsplash.com/photo-123"
	p.PhotographerURL = "https://unsplash.com/photographer"
	if err := ValidatePhoto(p); err != nil {
		t.Fatalf("optional metadata and ixid must not be invented: %v", err)
	}
	p.Photographer = ""
	if ValidatePhoto(p) == nil {
		t.Fatal("photo must have a photographer credit")
	}
}

func TestUnsplashTrack(t *testing.T) {
	var calls atomic.Int32
	u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.RequestURI != "/photos/abc_123-X/download?ixid=original%2Bvalue" ||
			r.Header.Get("Authorization") != "Client-ID explicit-test-key" || r.Header.Get("Accept-Version") != "v1" {
			t.Error("incorrect tracking request")
		}
		fmt.Fprint(w, `{"url":"https://never-fetch.example/image-bytes"}`)
	})
	if err := u.Track(context.Background(), validTestPhoto()); err != nil {
		t.Fatal(err)
	}
	p := validTestPhoto()
	p.DownloadURL = "https://localhost/photos/abc_123-X/download"
	if err := u.Track(context.Background(), p); err == nil {
		t.Fatal("accepted unsafe tracking URL")
	}
	p = validTestPhoto()
	p.URL = "https://evil.example/image"
	if err := u.Track(context.Background(), p); err == nil {
		t.Fatal("must validate complete photo before tracking")
	}
	if calls.Load() != 1 {
		t.Fatal("Track made an unexpected external request")
	}
}

func TestUnsplashResponseFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "private-query explicit-test-key provider-payload"},
		{"redirect", 302, "private-query"},
		{"invalid JSON", 200, "private-query"},
		{"trailing JSON", 200, `{"results":[]} {}`},
		{"oversized", 200, strings.Repeat(" ", 2*1024*1024+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var redirects atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
			defer target.Close()
			u := testUnsplash(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := u.Search(context.Background(), "private-query", "")
			if err == nil {
				t.Fatal("expected search failure")
			}
			for _, secret := range []string{"private-query", "explicit-test-key", "provider-payload", u.endpoint} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("error exposes private request/response data")
				}
			}
			var failure *photoError
			if !errors.As(err, &failure) || tc.status != 200 && failure.httpStatus != tc.status {
				t.Fatalf("missing safe status metadata: %v", err)
			}
			if err := u.Track(context.Background(), validTestPhoto()); err == nil {
				t.Fatal("expected tracking failure")
			}
			if redirects.Load() != 0 {
				t.Fatal("followed redirect")
			}
		})
	}
}

func TestUnsplashTimeoutAndCancellation(t *testing.T) {
	u := NewUnsplash("explicit-test-key")
	if u.client.Timeout != 15*time.Second || u.endpoint != "https://api.unsplash.com" {
		t.Fatal("incorrect production timeout or endpoint")
	}
	u = testUnsplash(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	u.client.Timeout = 20 * time.Millisecond
	if _, err := u.Search(context.Background(), "forest", ""); err == nil {
		t.Fatal("request did not time out")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.Search(ctx, "forest", ""); err == nil {
		t.Fatal("ignored canceled context")
	}
	if err := u.Track(ctx, validTestPhoto()); err == nil {
		t.Fatal("tracking ignored canceled context")
	}
	u.accessKey = ""
	if _, err := u.Search(context.Background(), "forest", ""); err == nil {
		t.Fatal("accepted missing explicit access key")
	}
}
