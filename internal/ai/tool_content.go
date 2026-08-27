package ai

import (
	"encoding/json"
	"strings"
)

type toolContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// ParseToolContent splits a tool result into text plus any embedded image blocks.
func ParseToolContent(raw string) (text string, images []ImageContent) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	var obj struct {
		Content []toolContentBlock `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil && len(obj.Content) > 0 {
		if t, imgs, ok := splitToolBlocks(obj.Content); ok {
			return t, imgs
		}
	}
	var arr []toolContentBlock
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		if t, imgs, ok := splitToolBlocks(arr); ok {
			return t, imgs
		}
	}
	return raw, nil
}

func splitToolBlocks(blocks []toolContentBlock) (string, []ImageContent, bool) {
	var texts []string
	var imgs []ImageContent
	found := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			found = true
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		case "image":
			found = true
			if b.Data != "" {
				imgs = append(imgs, ImageContent{Type: "image", Data: b.Data, MimeType: b.MimeType})
			}
		}
	}
	if !found {
		return "", nil, false
	}
	return strings.Join(texts, "\n"), imgs, true
}

const imageBlockedNote = "[Image blocked by settings.blockImages]"

// BlockImages returns a copy of msgs with image blocks removed for LLM requests.
func BlockImages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		m.Images = nil
		if text, imgs := ParseToolContent(m.Content); len(imgs) > 0 {
			if text == "" {
				m.Content = imageBlockedNote
			} else {
				m.Content = text + "\n" + imageBlockedNote
			}
		}
		out[i] = m
	}
	return out
}
