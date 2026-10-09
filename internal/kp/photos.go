package kp

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Photo struct {
	ID, URL, Alt, Photographer, PhotographerURL, PageURL, DownloadURL string
	Width, Height                                                     int
}

// ValidatePhoto is also used when reading persisted photos, before rendering URLs.
func ValidatePhoto(photo Photo) error {
	invalid := func() error { return errors.New("invalid photo") }
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
