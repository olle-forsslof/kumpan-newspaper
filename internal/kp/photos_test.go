package kp

import (
	"strings"
	"testing"
)

func validTestPhoto() Photo {
	return Photo{
		ID: "abc_123-X", URL: "https://images.unsplash.com/photo-123?ixid=original%2Bvalue&ixlib=rb-4.0.3",
		Alt: "A forest", Photographer: "A Photographer", PhotographerURL: "https://unsplash.com/@photographer",
		PageURL:     "https://unsplash.com/photos/forest-abc_123-X",
		DownloadURL: "https://api.unsplash.com/photos/abc_123-X/download?ixid=original%2Bvalue",
		Width:       6000, Height: 4000,
	}
}

func TestValidatePhoto(t *testing.T) {
	if err := ValidatePhoto(validTestPhoto()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Photo){
		"empty ID":                  func(p *Photo) { p.ID = "" },
		"long ID":                   func(p *Photo) { p.ID = strings.Repeat("x", 101) },
		"slash ID":                  func(p *Photo) { p.ID = "a/b" },
		"escaped ID":                func(p *Photo) { p.ID = "a%2Fb" },
		"unicode ID":                func(p *Photo) { p.ID = "\u00e9" },
		"zero width":                func(p *Photo) { p.Width = 0 },
		"negative height":           func(p *Photo) { p.Height = -1 },
		"large dimension":           func(p *Photo) { p.Width = 100001 },
		"long alt":                  func(p *Photo) { p.Alt = strings.Repeat("a", 2001) },
		"long photographer":         func(p *Photo) { p.Photographer = strings.Repeat("a", 501) },
		"invalid alt UTF8":          func(p *Photo) { p.Alt = string([]byte{255}) },
		"invalid photographer UTF8": func(p *Photo) { p.Photographer = string([]byte{255}) },
		"long URL":                  func(p *Photo) { p.URL += strings.Repeat("a", 4096) },
		"page path":                 func(p *Photo) { p.PageURL = "https://unsplash.com/not-a-photo" },
		"empty photographer path":   func(p *Photo) { p.PhotographerURL = "https://unsplash.com/" },
		"download mismatched ID":    func(p *Photo) { p.DownloadURL = "https://api.unsplash.com/photos/other/download" },
		"download escaped slash":    func(p *Photo) { p.DownloadURL = "https://api.unsplash.com/photos/abc_123-X%2Fdownload" },
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
		"https://images.unsplash.com/\\photo",
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
