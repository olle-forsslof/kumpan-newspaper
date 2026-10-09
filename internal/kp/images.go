package kp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const imageResponseLimit = 32 * 1024 * 1024

type ImageAsset struct {
	Name          string
	Width, Height int
}

type ImageService interface {
	Generate(ctx context.Context, kind, prompt string) (ImageAsset, error)
	Discard(asset ImageAsset) error
}

type AIImages struct {
	client                   *http.Client
	apiKey, model, directory string
	endpoint                 string
}

// Only fixed reasons and allowlisted provider codes may reach worker logs.
type imageError struct {
	reason     string
	httpStatus int
	code       string
}

func (e *imageError) Error() string { return e.reason }

func ValidateImageAsset(asset ImageAsset) error {
	if !validImageName(asset.Name) || asset.Width <= 0 || asset.Height <= 0 ||
		asset.Width > 4096 || asset.Height > 4096 || int64(asset.Width)*int64(asset.Height) > 16000000 {
		return &imageError{reason: "invalid image asset"}
	}
	return nil
}

func validImageName(name string) bool {
	if len(name) != 36 || (name[32:] != ".jpg" && name[32:] != ".png") {
		return false
	}
	for _, c := range name[:32] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func NewImageGenerator(apiKey, model, directory string) (*AIImages, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, &imageError{reason: "invalid image directory"}
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, &imageError{reason: "invalid image directory"}
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, &imageError{reason: "image directory unavailable"}
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, &imageError{reason: "invalid image directory"}
	}
	if err = os.Chmod(root, 0700); err != nil {
		return nil, &imageError{reason: "image directory unavailable"}
	}
	probe, err := os.CreateTemp(root, ".image-probe-")
	if err != nil {
		return nil, &imageError{reason: "image directory not writable"}
	}
	closeErr := probe.Close()
	removeErr := os.Remove(probe.Name())
	if closeErr != nil || removeErr != nil {
		return nil, &imageError{reason: "image directory not writable"}
	}
	if err := syncImageDirectory(root); err != nil {
		return nil, err
	}
	if err := syncImageDirectory(filepath.Dir(root)); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		model = "gpt-image-2.5-flare"
	}
	return &AIImages{
		apiKey: apiKey, model: model, directory: root,
		endpoint: "https://api.openai.com/v1/images/generations",
		client: &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (a *AIImages) Generate(ctx context.Context, kind, prompt string) (ImageAsset, error) {
	fail := func(reason string) (ImageAsset, error) { return ImageAsset{}, &imageError{reason: reason} }
	if kind != "report" && kind != "question" {
		return fail("invalid image kind")
	}
	if ValidateImagePrompt(prompt) != nil {
		return fail("invalid image prompt")
	}
	if strings.TrimSpace(a.apiKey) == "" {
		return fail("image service not configured")
	}
	background, style := "opaque", "Black-and-white photography with subtle grain. Make it feel unnatural."
	if kind == "question" {
		background, style = "transparent", "Drawing on a transparent background. No color. Pencil. Leonardo Da Vinci. Modigliani. Tove Janson"
	}
	fullPrompt := style + "\nScene content:\n" + prompt + "\nMandatory style and safety: " + style +
		" Use only generic fictional people, with no resemblance to real employees or other real people. No text, lettering, logos, or watermarks."
	body, err := json.Marshal(struct {
		Model        string `json:"model"`
		Prompt       string `json:"prompt"`
		Quality      string `json:"quality"`
		Size         string `json:"size"`
		Background   string `json:"background"`
		N            int    `json:"n"`
		OutputFormat string `json:"output_format"`
	}{Model: a.model, Prompt: fullPrompt, Quality: "medium", Size: "1536x1024", Background: background, N: 1, OutputFormat: "png"})
	if err != nil {
		return fail("image request failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return fail("image request failed")
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(req)
	if err != nil {
		return fail("image request failed")
	}
	defer response.Body.Close()
	body, err = io.ReadAll(io.LimitReader(response.Body, imageResponseLimit+1))
	if err != nil {
		return fail("image response read failed")
	}
	if len(body) > imageResponseLimit {
		return fail("image response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := &imageError{reason: "image request failed", httpStatus: response.StatusCode}
		var payload struct{ Error struct{ Code string } }
		if json.Unmarshal(body, &payload) == nil {
			switch payload.Error.Code {
			case "insufficient_quota", "rate_limit_exceeded", "invalid_api_key", "billing_hard_limit_reached", "content_policy_violation", "model_not_found":
				failure.code = payload.Error.Code
			}
		}
		return ImageAsset{}, failure
	}
	var result struct {
		Data []struct {
			Base64 string `json:"b64_json"`
		}
	}
	if json.Unmarshal(body, &result) != nil || len(result.Data) != 1 || result.Data[0].Base64 == "" {
		return fail("invalid image response")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(result.Data[0].Base64)
	if err != nil || len(raw) > imageResponseLimit {
		return fail("invalid image encoding")
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 ||
		int64(config.Width)*int64(config.Height) > 16000000 {
		return fail("invalid image dimensions or format")
	}
	if ctx.Err() != nil {
		return fail("image generation canceled")
	}
	source, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return fail("invalid image encoding")
	}
	converted := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
	for y := 0; y < config.Height; y++ {
		if ctx.Err() != nil {
			return fail("image generation canceled")
		}
		for x := 0; x < config.Width; x++ {
			c := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			luma := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B) + 500) / 1000
			if kind == "report" {
				gray := uint8((luma*uint32(c.A) + 255*(255-uint32(c.A)) + 127) / 255)
				converted.SetNRGBA(x, y, color.NRGBA{R: gray, G: gray, B: gray, A: 255})
			} else {
				converted.SetNRGBA(x, y, color.NRGBA{A: uint8(uint32(c.A) * (255 - luma) / 255)})
			}
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fail("image name creation failed")
	}
	ext := ".jpg"
	if kind == "question" {
		ext = ".png"
	}
	asset := ImageAsset{Name: hex.EncodeToString(random[:]) + ext, Width: config.Width, Height: config.Height}
	var created []string
	var temporary []string
	complete := false
	defer func() {
		for _, path := range temporary {
			_ = os.Remove(path)
		}
		if !complete {
			for _, path := range created {
				_ = os.Remove(path)
			}
		}
	}()
	for _, width := range []int{0, 400, 800} {
		if ctx.Err() != nil {
			return fail("image generation canceled")
		}
		img := converted
		if width != 0 {
			img, err = resizeImage(ctx, converted, width)
			if err != nil {
				if failure, ok := err.(*imageError); ok {
					return ImageAsset{}, failure
				}
				return fail("image generation canceled")
			}
		}
		tmp, err := os.CreateTemp(a.directory, ".image-")
		if err != nil {
			return fail("image file write failed")
		}
		temporary = append(temporary, tmp.Name())
		if err := tmp.Chmod(0600); err != nil {
			_ = tmp.Close()
			return fail("image file write failed")
		}
		if kind == "report" {
			// Gray encoding guarantees equal RGB channels even after JPEG decoding.
			gray := image.NewGray(img.Bounds())
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					gray.SetGray(x, y, color.Gray{Y: img.NRGBAAt(x, y).R})
				}
			}
			err = jpeg.Encode(tmp, gray, &jpeg.Options{Quality: 85})
		} else {
			err = png.Encode(tmp, img)
		}
		if err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err != nil || closeErr != nil {
			return fail("image file write failed")
		}
		if ctx.Err() != nil {
			return fail("image generation canceled")
		}
		path := filepath.Join(a.directory, imageVariantName(asset.Name, width))
		// Hard linking publishes the complete file atomically and never replaces a name.
		if err = os.Link(tmp.Name(), path); err != nil {
			return fail("image file write failed")
		}
		created = append(created, path)
	}
	if ctx.Err() != nil {
		return fail("image generation canceled")
	}
	for len(temporary) > 0 {
		if err := os.Remove(temporary[0]); err != nil {
			return fail("image file write failed")
		}
		temporary = temporary[1:]
	}
	// Persist all final links and temporary-name removals before SQL can reference the asset.
	if err := syncImageDirectory(a.directory); err != nil {
		return ImageAsset{}, err
	}
	if ctx.Err() != nil {
		return fail("image generation canceled")
	}
	complete = true
	return asset, nil
}

func syncImageDirectory(directory string) error {
	failure := &imageError{reason: "image directory sync failed"}
	dir, err := os.Open(directory)
	if err != nil {
		return failure
	}
	info, err := dir.Stat()
	if err != nil || !info.IsDir() {
		_ = dir.Close()
		return failure
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil || closeErr != nil {
		return failure
	}
	return nil
}

// Area averaging avoids aliasing on downsampled ink lines. Smaller provider
// results use bilinear interpolation, preserving black RGB and smooth alpha.
func resizeImage(ctx context.Context, src *image.NRGBA, width int) (*image.NRGBA, error) {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	height := max(1, (sh*width+sw/2)/sw)
	if int64(width)*int64(height) > 16000000 {
		return nil, &imageError{reason: "invalid image variant dimensions"}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	upsample := width > sw
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		y0, y1 := float64(y)*float64(sh)/float64(height), float64(y+1)*float64(sh)/float64(height)
		yCenter := math.Max(0, math.Min(float64(sh-1), (float64(y)+0.5)*float64(sh)/float64(height)-0.5))
		if upsample {
			y0, y1 = math.Floor(yCenter), math.Min(float64(sh), math.Floor(yCenter)+2)
		}
		for x := 0; x < width; x++ {
			x0, x1 := float64(x)*float64(sw)/float64(width), float64(x+1)*float64(sw)/float64(width)
			xCenter := math.Max(0, math.Min(float64(sw-1), (float64(x)+0.5)*float64(sw)/float64(width)-0.5))
			if upsample {
				x0, x1 = math.Floor(xCenter), math.Min(float64(sw), math.Floor(xCenter)+2)
			}
			var sum [4]float64
			var area float64
			for sy := int(y0); sy < min(sh, int(math.Ceil(y1))); sy++ {
				wy := math.Min(y1, float64(sy+1)) - math.Max(y0, float64(sy))
				if upsample {
					wy = 1 - math.Abs(float64(sy)-yCenter)
				}
				for sx := int(x0); sx < min(sw, int(math.Ceil(x1))); sx++ {
					wx := math.Min(x1, float64(sx+1)) - math.Max(x0, float64(sx))
					if upsample {
						wx = 1 - math.Abs(float64(sx)-xCenter)
					}
					weight := wy * wx
					area += weight
					c := src.NRGBAAt(sx, sy)
					for i, v := range [4]uint8{c.R, c.G, c.B, c.A} {
						sum[i] += float64(v) * weight
					}
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(sum[0]/area + 0.5), G: uint8(sum[1]/area + 0.5), B: uint8(sum[2]/area + 0.5), A: uint8(sum[3]/area + 0.5)})
		}
	}
	return dst, nil
}

func imageVariantName(name string, width int) string {
	if width == 0 || width == 1536 {
		return name
	}
	return name[:32] + "-" + strconv.Itoa(width) + name[32:]
}

func (a *AIImages) Discard(asset ImageAsset) error {
	if err := ValidateImageAsset(asset); err != nil {
		return err
	}
	var failure error
	for _, width := range []int{0, 400, 800} {
		if err := os.Remove(filepath.Join(a.directory, imageVariantName(asset.Name, width))); err != nil && !os.IsNotExist(err) {
			failure = &imageError{reason: "image discard failed"}
		}
	}
	return failure
}

func OpenImageFile(directory, name string, width int) (*os.File, error) {
	if !validImageName(name) || (width != 0 && width != 400 && width != 800 && width != 1536) {
		return nil, &imageError{reason: "invalid image file"}
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, &imageError{reason: "image file unavailable"}
	}
	path := filepath.Join(directory, imageVariantName(name, width))
	// O_NOFOLLOW closes the leaf-symlink race; O_NONBLOCK avoids hanging on FIFOs.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &imageError{reason: "image file unavailable"}
	}
	f := os.NewFile(uintptr(fd), path)
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, &imageError{reason: "image file unavailable"}
	}
	return f, nil
}
