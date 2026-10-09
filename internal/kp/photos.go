package kp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Photo struct {
	ID, URL, Alt, Photographer, PhotographerURL, PageURL, DownloadURL string
	Width, Height                                                     int
}

type PhotoService interface {
	Search(ctx context.Context, query, excludeID string) (Photo, error)
	Track(ctx context.Context, photo Photo) error
}

var ErrNoPhoto = errors.New("no suitable photo found")

type Unsplash struct {
	client    *http.Client
	accessKey string
	endpoint  string
}

// Provider payloads, URLs, queries and credentials must never reach error logs.
type photoError struct {
	reason     string
	httpStatus int
}

func (e *photoError) Error() string { return e.reason }

func NewUnsplash(accessKey string) *Unsplash {
	return &Unsplash{
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		accessKey: accessKey,
		endpoint:  "https://api.unsplash.com",
	}
}

// ValidatePhoto is also used when reading persisted photos, before rendering URLs.
func ValidatePhoto(photo Photo) error {
	invalid := func() error { return &photoError{reason: "invalid photo"} }
	if len(photo.ID) < 1 || len(photo.ID) > 100 {
		return invalid()
	}
	for _, c := range photo.ID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return invalid()
		}
	}
	if strings.TrimSpace(photo.Photographer) == "" || photo.Width <= 0 || photo.Height <= 0 || photo.Width > 100000 || photo.Height > 100000 ||
		len(photo.Alt) > 2000 || len(photo.Photographer) > 500 || !utf8.ValidString(photo.Alt) || !utf8.ValidString(photo.Photographer) {
		return invalid()
	}
	for _, link := range []struct{ value, host, kind string }{
		{photo.URL, "images.unsplash.com", "image"},
		{photo.PageURL, "unsplash.com", "page"},
		{photo.PhotographerURL, "unsplash.com", "photographer"},
		{photo.DownloadURL, "api.unsplash.com", "download"},
	} {
		if len(link.value) == 0 || len(link.value) > 4096 || !utf8.ValidString(link.value) || strings.ContainsAny(link.value, "\\#") {
			return invalid()
		}
		for _, c := range link.value {
			if unicode.IsControl(c) || unicode.IsSpace(c) {
				return invalid()
			}
		}
		u, err := url.Parse(link.value)
		if err != nil || u.Scheme != "https" || u.Host != link.host || u.User != nil || u.Fragment != "" || u.Opaque != "" {
			return invalid()
		}
		params, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return invalid()
		}
		for key := range params {
			if strings.EqualFold(key, "client_id") || strings.EqualFold(key, "access_token") {
				return invalid()
			}
		}
		switch link.kind {
		case "page":
			if !strings.HasPrefix(u.Path, "/photos/") || len(u.Path) == len("/photos/") {
				return invalid()
			}
		case "photographer":
			if !strings.HasPrefix(u.Path, "/") || len(u.Path) < 2 {
				return invalid()
			}
		case "download":
			// Require the literal ID path, not an encoded slash or alternate spelling.
			if u.RawPath != "" || u.Path != "/photos/"+photo.ID+"/download" {
				return invalid()
			}
		}
	}
	return nil
}

func (u *Unsplash) Search(ctx context.Context, query, excludeID string) (Photo, error) {
	if ValidateImageQuery(query) != nil {
		return Photo{}, &photoError{reason: "invalid photo query"}
	}
	query = strings.TrimSpace(query)
	params := url.Values{
		"query": {query}, "orientation": {"landscape"}, "content_filter": {"high"},
		"per_page": {"10"}, "page": {"1"},
	}
	body, err := u.get(ctx, "/search/photos?"+params.Encode())
	if err != nil {
		return Photo{}, err
	}
	var result struct {
		Results []struct {
			ID             string
			Width, Height  int
			AltDescription string `json:"alt_description"`
			Description    string
			URLs           struct{ Raw string }
			Links          struct {
				HTML             string
				DownloadLocation string `json:"download_location"`
			}
			User struct {
				Name, Username string
				Links          struct{ HTML string }
			}
		}
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Photo{}, &photoError{reason: "invalid photo response"}
	}
	for _, item := range result.Results {
		alt := item.AltDescription
		if alt == "" {
			alt = item.Description
		}
		photographer := item.User.Name
		if strings.TrimSpace(photographer) == "" {
			photographer = item.User.Username
		}
		photo := Photo{
			ID: item.ID, URL: item.URLs.Raw, Alt: alt,
			Photographer: photographer, PhotographerURL: item.User.Links.HTML,
			PageURL: item.Links.HTML, DownloadURL: item.Links.DownloadLocation,
			Width: item.Width, Height: item.Height,
		}
		if photo.ID != excludeID && ValidatePhoto(photo) == nil {
			return photo, nil
		}
	}
	return Photo{}, ErrNoPhoto
}

func (u *Unsplash) Track(ctx context.Context, photo Photo) error {
	if err := ValidatePhoto(photo); err != nil {
		return err
	}
	link, _ := url.Parse(photo.DownloadURL)
	// Only the validated API path is used; the returned download URL is never fetched.
	body, err := u.get(ctx, link.RequestURI())
	if err != nil {
		return err
	}
	if !json.Valid(body) {
		return &photoError{reason: "invalid photo response"}
	}
	return nil
}

func (u *Unsplash) get(ctx context.Context, path string) ([]byte, error) {
	if strings.TrimSpace(u.accessKey) == "" {
		return nil, &photoError{reason: "photo service not configured"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.endpoint+path, nil)
	if err != nil {
		return nil, &photoError{reason: "photo request failed"}
	}
	req.Header.Set("Authorization", "Client-ID "+u.accessKey)
	req.Header.Set("Accept-Version", "v1")
	response, err := u.client.Do(req)
	if err != nil {
		return nil, &photoError{reason: "photo request failed"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &photoError{reason: "photo request failed", httpStatus: response.StatusCode}
	}
	const limit = 2 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, &photoError{reason: "photo response read failed"}
	}
	if len(body) > limit {
		return nil, &photoError{reason: "photo response exceeds size limit"}
	}
	return body, nil
}
