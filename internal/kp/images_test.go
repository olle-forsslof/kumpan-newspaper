package kp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

var _ ImageService = (*AIImages)(nil)

func testAIImages(t *testing.T, handler http.HandlerFunc) *AIImages {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, err := NewImageGenerator("explicit-test-key", "test-model", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.endpoint = server.URL + "/v1/images/generations"
	return a
}

func imageTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	colors := []color.NRGBA{{R: 255, G: 255, B: 255, A: 255}, {A: 255}, {R: 255, A: 128}, {R: 30, G: 40, B: 50}}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			im.SetNRGBA(x, y, colors[x%len(colors)])
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func writeImageResponse(w http.ResponseWriter, raw []byte) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
}

func TestImageGeneratorConfiguration(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "environment-key")
	t.Setenv("OPENAI_BASE_URL", "http://localhost/never-used")
	directory := filepath.Join(t.TempDir(), "images")
	a, err := NewImageGenerator("explicit", "", directory)
	if err != nil {
		t.Fatal(err)
	}
	if a.apiKey != "explicit" || a.model != "gpt-image-2.5-flare" || a.endpoint != "https://api.openai.com/v1/images/generations" || a.client.Timeout != 3*time.Minute || !filepath.IsAbs(a.directory) {
		t.Fatal("incorrect production configuration")
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("root must have permissions 0700")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("writability probe leaked")
	}
	if _, err := NewImageGenerator("", "", ""); err == nil {
		t.Fatal("accepted empty root")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewImageGenerator("key", "", link); err == nil {
		t.Fatal("accepted root symlink")
	}
}

func TestImageGenerateStylesAndFiles(t *testing.T) {
	raw := imageTestPNG(t, 1536, 1024)
	for _, kind := range []string{"report", "question"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer explicit-test-key" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("incorrect image request")
				}
				var p map[string]any
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
				}
				bg, style := "opaque", "Black-and-white editorial newspaper photography with subtle grain."
				if kind == "question" {
					bg, style = "transparent", "Simple black ink line cartoon"
				}
				for k, want := range map[string]any{"model": "test-model", "n": float64(1), "quality": "medium", "size": "1536x1024", "output_format": "png", "background": bg} {
					if p[k] != want {
						t.Errorf("%s = %v, want %v", k, p[k], want)
					}
				}
				prompt, _ := p["prompt"].(string)
				for _, want := range []string{style, "Scene content:\nA fictional team\nin an office", "Mandatory style and safety:", "generic fictional people", "no resemblance to real employees", "No text, lettering, logos"} {
					if !strings.Contains(prompt, want) {
						t.Errorf("missing prompt constraint %q", want)
					}
				}
				if kind == "question" && !strings.Contains(prompt, "No color, fills, shading, gradients, or text") {
					t.Error("missing cartoon constraints")
				}
				writeImageResponse(w, raw)
			})
			asset, err := a.Generate(context.Background(), kind, "A fictional team\nin an office")
			if err != nil {
				t.Fatal(err)
			}
			if ValidateImageAsset(asset) != nil || asset.Width != 1536 || asset.Height != 1024 {
				t.Fatalf("invalid asset: %+v", asset)
			}
			ext := ".jpg"
			if kind == "question" {
				ext = ".png"
			}
			if !strings.HasSuffix(asset.Name, ext) {
				t.Fatal("wrong format")
			}
			entries, _ := os.ReadDir(a.directory)
			if len(entries) != 3 {
				t.Fatalf("expected only 3 completed files, got %d", len(entries))
			}
			for _, width := range []int{0, 400, 800, 1536} {
				f, err := OpenImageFile(a.directory, asset.Name, width)
				if err != nil {
					t.Fatal(err)
				}
				info, _ := f.Stat()
				if info.Mode().Perm() != 0600 {
					t.Error("file permissions must be 0600")
				}
				var im image.Image
				if kind == "report" {
					im, err = jpeg.Decode(f)
				} else {
					im, err = png.Decode(f)
				}
				_ = f.Close()
				if err != nil {
					t.Fatal(err)
				}
				wantW := width
				if width == 0 {
					wantW = 1536
				}
				wantH := (1024*wantW + 1536/2) / 1536
				if im.Bounds().Dx() != wantW || im.Bounds().Dy() != wantH {
					t.Errorf("variant %d dimensions = %v", width, im.Bounds())
				}
				for y := 0; y < im.Bounds().Dy(); y++ {
					for x := 0; x < im.Bounds().Dx(); x++ {
						c := color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
						if kind == "report" && (c.R != c.G || c.G != c.B || c.A != 255) {
							t.Fatal("news JPEG is not grayscale opaque")
						}
						if kind == "question" && (c.R != 0 || c.G != 0 || c.B != 0) {
							t.Fatal("cartoon contains nonblack RGB")
						}
					}
				}
				if kind == "question" && width == 0 {
					for x, alpha := range []uint8{0, 255, 89, 0} {
						c := color.NRGBAModel.Convert(im.At(x, 0)).(color.NRGBA)
						if c.A != alpha {
							t.Errorf("pixel %d alpha = %d, want %d", x, c.A, alpha)
						}
					}
				}
			}
			if calls.Load() != 1 {
				t.Fatal("unexpected requests")
			}
		})
	}
}

func TestValidateImageAsset(t *testing.T) {
	valid := ImageAsset{Name: strings.Repeat("a", 32) + ".jpg", Width: 1536, Height: 1024}
	if err := ValidateImageAsset(valid); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "../" + valid.Name, strings.Repeat("A", 32) + ".jpg", strings.Repeat("g", 32) + ".png", valid.Name + "?x=1", strings.Repeat("a", 32) + ".jpeg", strings.Repeat("a", 32) + "-400.jpg", "https://example.com/a.png"} {
		p := valid
		p.Name = name
		if ValidateImageAsset(p) == nil {
			t.Errorf("accepted name %q", name)
		}
	}
	for _, dims := range [][2]int{{0, 1}, {1, -1}, {4097, 1}, {4096, 4096}, {int(^uint(0) >> 1), 2}} {
		p := valid
		p.Width, p.Height = dims[0], dims[1]
		if ValidateImageAsset(p) == nil {
			t.Errorf("accepted dimensions %v", dims)
		}
	}
	valid.Name = strings.Repeat("0", 32) + ".png"
	valid.Width, valid.Height = 4000, 4000
	if err := ValidateImageAsset(valid); err != nil {
		t.Fatal(err)
	}
}

func TestImageInputValidation(t *testing.T) {
	var calls atomic.Int32
	a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, prompt := range []string{"", " \n ", strings.Repeat("a", 3001), strings.Repeat("\u00e9", 1501), "a\x00b", "a\tb", "a\rb", "a\x7fb", string([]byte{255})} {
		if _, err := a.Generate(context.Background(), "report", prompt); err == nil {
			t.Errorf("accepted invalid prompt %q", prompt)
		}
	}
	for _, kind := range []string{"", "news", "REPORT", "../../report"} {
		if _, err := a.Generate(context.Background(), kind, "office"); err == nil {
			t.Errorf("accepted kind %q", kind)
		}
	}
	a.apiKey = ""
	if _, err := a.Generate(context.Background(), "report", "office"); err == nil {
		t.Fatal("accepted empty API key")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input reached API")
	}
}

func TestImageResponseFailures(t *testing.T) {
	oversizePNG := imageTestPNG(t, 1, 1)
	// Patch the IHDR and its CRC, so DecodeConfig rejects dimensions before allocation.
	binary.BigEndian.PutUint32(oversizePNG[16:20], 4097)
	binary.BigEndian.PutUint32(oversizePNG[29:33], crc32.ChecksumIEEE(oversizePNG[12:29]))
	overPixelsPNG := append([]byte(nil), oversizePNG...)
	binary.BigEndian.PutUint32(overPixelsPNG[16:20], 4096)
	binary.BigEndian.PutUint32(overPixelsPNG[20:24], 4096)
	binary.BigEndian.PutUint32(overPixelsPNG[29:33], crc32.ChecksumIEEE(overPixelsPNG[12:29]))
	var jpegBytes bytes.Buffer
	_ = jpeg.Encode(&jpegBytes, image.NewGray(image.Rect(0, 0, 2, 2)), nil)
	encoded := func(raw []byte) string {
		return `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(raw) + `"}]}`
	}
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
	}{
		{"quota", 429, `{"error":{"code":"insufficient_quota","message":"private-prompt explicit-test-key"}}`, "insufficient_quota"},
		{"private code", 400, `{"error":{"code":"private-prompt explicit-test-key"}}`, ""},
		{"redirect", 302, "private-prompt", ""},
		{"invalid JSON", 200, "private-prompt", ""},
		{"trailing JSON", 200, `{"data":[]} {}`, ""},
		{"empty", 200, `{"data":[]}`, ""},
		{"multiple", 200, `{"data":[{"b64_json":"AA=="},{"b64_json":"AA=="}]}`, ""},
		{"URL only", 200, `{"data":[{"url":"http://127.0.0.1/private"}]}`, ""},
		{"bad base64", 200, `{"data":[{"b64_json":"!!!"}]}`, ""},
		{"noncanonical base64", 200, `{"data":[{"b64_json":"AB=="}]}`, ""},
		{"not PNG", 200, encoded(jpegBytes.Bytes()), ""},
		{"corrupt PNG", 200, encoded(imageTestPNG(t, 2, 2)[:33]), ""},
		{"oversized dimensions", 200, encoded(oversizePNG), ""},
		{"oversized pixels", 200, encoded(overPixelsPNG), ""},
		{"oversized response", 200, strings.Repeat(" ", imageResponseLimit+1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var followed atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed.Add(1) }))
			defer target.Close()
			a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := a.Generate(context.Background(), "report", "private-prompt")
			if err == nil {
				t.Fatal("expected failure")
			}
			var failure *imageError
			if !errors.As(err, &failure) || failure.code != tc.code {
				t.Fatalf("missing safe error metadata: %v", err)
			}
			if tc.status != 200 && failure.httpStatus != tc.status {
				t.Fatal("missing status")
			}
			for _, secret := range []string{"private-prompt", "explicit-test-key", a.endpoint, a.directory} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("private data leaked")
				}
			}
			if followed.Load() != 0 {
				t.Fatal("followed redirect")
			}
			entries, _ := os.ReadDir(a.directory)
			if len(entries) != 0 {
				t.Fatal("failure leaked files")
			}
		})
	}
}

func TestImageTimeoutCancellation(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	a.client.Timeout = 20 * time.Millisecond
	if _, err := a.Generate(context.Background(), "report", "office"); err == nil {
		t.Fatal("request did not time out")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Generate(ctx, "report", "office"); err == nil {
		t.Fatal("ignored cancellation")
	}
	entries, _ := os.ReadDir(a.directory)
	if len(entries) != 0 {
		t.Fatal("cancellation leaked files")
	}
}

func TestImageDiscardAndFileAccess(t *testing.T) {
	raw := imageTestPNG(t, 60, 40)
	a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) { writeImageResponse(w, raw) })
	first, err := a.Generate(context.Background(), "question", "office")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Generate(context.Background(), "question", "office")
	if err != nil {
		t.Fatal(err)
	}
	if first.Name == second.Name {
		t.Fatal("reused name")
	}
	if err := a.Discard(first); err != nil {
		t.Fatal(err)
	}
	if err := a.Discard(first); err != nil {
		t.Fatal("discard must tolerate missing files")
	}
	for _, width := range []int{0, 400, 800} {
		if _, err := OpenImageFile(a.directory, first.Name, width); err == nil {
			t.Fatal("discard left variant")
		}
		f, err := OpenImageFile(a.directory, second.Name, width)
		if err != nil {
			t.Fatal("discard removed another asset")
		}
		_ = f.Close()
	}
	invalid := second
	invalid.Name = "../" + second.Name
	if a.Discard(invalid) == nil {
		t.Fatal("accepted unsafe discard")
	}
	for _, name := range []string{"../" + second.Name, second.Name + "/", "http://localhost/a.png", ".", ""} {
		if _, err := OpenImageFile(a.directory, name, 0); err == nil {
			t.Errorf("accepted unsafe name %q", name)
		}
	}
	for _, width := range []int{-1, 1, 1024, 4096} {
		if _, err := OpenImageFile(a.directory, second.Name, width); err == nil {
			t.Errorf("accepted width %d", width)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenImageFile(missing, second.Name, 0); err == nil {
		t.Fatal("accepted missing root")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("GET created root")
	}
	path := filepath.Join(a.directory, second.Name)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(a.directory, imageVariantName(second.Name, 400))
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenImageFile(a.directory, second.Name, 0); err == nil {
		t.Fatal("accepted symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenImageFile(a.directory, second.Name, 0); err == nil {
		t.Fatal("accepted directory")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenImageFile(a.directory, second.Name, 0); err == nil {
		t.Fatal("accepted FIFO")
	}
}

type imageJobContext struct {
	context.Context
	check func() error
}

func (c imageJobContext) Err() error { return c.check() }

func TestImagePartialJobCleanup(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(fmt.Sprint("cancel=", cancel), func(t *testing.T) {
			raw := imageTestPNG(t, 60, 40)
			a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) { writeImageResponse(w, raw) })
			selected, err := a.Generate(context.Background(), "question", "selected image")
			if err != nil {
				t.Fatal(err)
			}
			var blocked string
			var triggered bool
			ctx := imageJobContext{Context: context.Background(), check: func() error {
				entries, err := os.ReadDir(a.directory)
				if err != nil {
					return err
				}
				for _, entry := range entries {
					if validImageName(entry.Name()) && entry.Name() != selected.Name {
						triggered = true
						if cancel {
							return context.Canceled
						}
						if blocked == "" {
							blocked = filepath.Join(a.directory, imageVariantName(entry.Name(), 400))
							if err := os.WriteFile(blocked, []byte("preexisting variant"), 0600); err != nil {
								return err
							}
						}
					}
				}
				return nil
			}}
			asset, err := a.Generate(ctx, "question", "new image")
			if err == nil || asset != (ImageAsset{}) || !triggered {
				t.Fatalf("expected failure after partial write: %+v, %v", asset, err)
			}
			entries, err := os.ReadDir(a.directory)
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 3
			if !cancel {
				wantCount++
				body, err := os.ReadFile(blocked)
				if err != nil || string(body) != "preexisting variant" {
					t.Fatal("overwrote or removed an unowned file")
				}
			}
			if len(entries) != wantCount {
				t.Fatalf("job files leaked: %v", entries)
			}
			for _, width := range []int{0, 400, 800} {
				f, err := OpenImageFile(a.directory, selected.Name, width)
				if err != nil {
					t.Fatal("cleanup removed selected image")
				}
				_ = f.Close()
			}
		})
	}
}

func TestImageCancellationBeforeFinalSuccess(t *testing.T) {
	raw := imageTestPNG(t, 60, 40)
	a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) { writeImageResponse(w, raw) })
	var canceled bool
	ctx := imageJobContext{Context: context.Background(), check: func() error {
		entries, err := os.ReadDir(a.directory)
		if err != nil {
			return err
		}
		if len(entries) == 3 {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".image-") {
					return nil
				}
			}
			canceled = true
			return context.Canceled
		}
		return nil
	}}
	asset, err := a.Generate(ctx, "question", "office")
	if err == nil || asset != (ImageAsset{}) || !canceled {
		t.Fatalf("expected cancellation after temporary cleanup: %+v, %v", asset, err)
	}
	entries, err := os.ReadDir(a.directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancellation left job files: %v, %v", entries, err)
	}
}

func TestSyncImageDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := syncImageDirectory(directory); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "not-a-directory")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(directory, "missing"), file} {
		err := syncImageDirectory(path)
		var failure *imageError
		if !errors.As(err, &failure) || failure.reason != "image directory sync failed" || strings.Contains(err.Error(), directory) {
			t.Fatalf("expected safe directory-sync failure: %v", err)
		}
	}
}

func TestImageAreaResampling(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1600, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 1600; x++ {
			src.SetNRGBA(x, y, color.NRGBA{A: uint8((x % 2) * 255)})
		}
	}
	dst, err := resizeImage(context.Background(), src, 400)
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 400; x++ {
		if c := dst.NRGBAAt(x, 0); c != (color.NRGBA{A: 128}) {
			t.Fatalf("resampling aliased thin lines: %+v", c)
		}
	}
	if _, err := resizeImage(context.Background(), image.NewNRGBA(image.Rect(0, 0, 1, 4096)), 800); err == nil {
		t.Fatal("unbounded variant allocation")
	}
	small := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	small.SetNRGBA(1, 0, color.NRGBA{A: 255})
	large, err := resizeImage(context.Background(), small, 4)
	if err != nil {
		t.Fatal(err)
	}
	for x, alpha := range []uint8{0, 64, 191, 255} {
		if c := large.NRGBAAt(x, 0); c != (color.NRGBA{A: alpha}) {
			t.Fatalf("upsampling is not bilinear: %+v", c)
		}
	}
}

func TestImageResponseURLNeverFetched(t *testing.T) {
	var fetched atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fetched.Add(1) }))
	defer target.Close()
	raw := imageTestPNG(t, 60, 40)
	for _, withBytes := range []bool{false, true} {
		a := testAIImages(t, func(w http.ResponseWriter, r *http.Request) {
			item := map[string]string{"url": target.URL + "/private-image"}
			if withBytes {
				item["b64_json"] = base64.StdEncoding.EncodeToString(raw)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{item}})
		})
		_, err := a.Generate(context.Background(), "report", "office")
		if withBytes && err != nil || !withBytes && err == nil {
			t.Fatalf("incorrect response handling: %v", err)
		}
	}
	if fetched.Load() != 0 {
		t.Fatal("fetched provider response URL")
	}
}
