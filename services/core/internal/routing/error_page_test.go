package routing

import (
	"bytes"
	"encoding/base64"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestErrorPagesEmbedVisibleImages(t *testing.T) {
	imageSource := regexp.MustCompile(`src="([^"]+)"`)
	for _, kind := range []string{"offline", "port", "service", "not-found", "forbidden", "internal"} {
		t.Run(kind, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
			response := httptest.NewRecorder()
			(&Router{}).showError(response, request, kind)

			if response.Code != routeErrors[kind].status {
				t.Fatalf("unexpected status: %d", response.Code)
			}
			if bytes.Contains(response.Body.Bytes(), []byte("#ZgotmplZ")) {
				t.Fatal("html/template rejected an image URL")
			}
			if !bytes.Contains(response.Body.Bytes(), []byte(`class="brand-wordmark">Cloud<span class="brand-forge">forge</span>`)) {
				t.Fatal("brand wordmark is not grouped")
			}
			if count := bytes.Count(response.Body.Bytes(), []byte("data:font/woff2;base64,")); count != 5 {
				t.Fatalf("expected five embedded fonts, got %d", count)
			}
			images := imageSource.FindAllSubmatch(response.Body.Bytes(), -1)
			if len(images) != 2 {
				t.Fatalf("expected brand mark and mascot images, got %d", len(images))
			}
			for _, image := range images {
				source := html.UnescapeString(string(image[1]))
				encoded := strings.TrimPrefix(source, "data:image/webp;base64,")
				if encoded == source {
					t.Fatalf("unexpected image source: %s", source)
				}
				data, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil || len(data) < 12 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
					t.Fatal("invalid embedded WebP image")
				}
			}
		})
	}
}
