package llm

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ContentPart represents a single part of a multimodal message content array.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds the URL for an image content part (supports base64 data URIs).
type ImageURL struct {
	URL string `json:"url"`
}

// MultimodalMessage is a chat message whose content is an array of ContentPart,
// used for vision requests that mix text and images.
type MultimodalMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// TextPart creates a text-only ContentPart.
func TextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

// ImageBase64Part creates an image ContentPart from raw base64 data and MIME type.
func ImageBase64Part(mime string, b64 string) ContentPart {
	return ContentPart{
		Type: "image_url",
		ImageURL: &ImageURL{
			URL: fmt.Sprintf("data:%s;base64,%s", mime, b64),
		},
	}
}

// LoadImageAsBase64 reads a local image file and returns a base64-encoded ContentPart.
func LoadImageAsBase64(filePath string) (ContentPart, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ContentPart{}, fmt.Errorf("read image %s: %w", filePath, err)
	}

	mime := mimeFromExt(filepath.Ext(filePath))
	b64 := base64.StdEncoding.EncodeToString(data)
	return ImageBase64Part(mime, b64), nil
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
