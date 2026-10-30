package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

type toolContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

const errorToolImagePlaceholder = "(image attached after the tool results)"

// ParseToolContent splits a tool result into text plus any embedded image blocks.
func ParseToolContent(raw string) (text string, images []ImageContent) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if blocks, ok := toolContentBlocks(raw); ok {
		text, images, _ = splitToolBlocks(blocks)
		return text, images
	}
	return raw, nil
}

func toolContentBlocks(raw string) ([]toolContentBlock, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	var obj struct {
		Content []toolContentBlock `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil && len(obj.Content) > 0 {
		if _, _, ok := splitToolBlocks(obj.Content); ok {
			return obj.Content, true
		}
	}
	var arr []toolContentBlock
	if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
		if _, _, ok := splitToolBlocks(arr); ok {
			return arr, true
		}
	}
	return nil, false
}

// splitErrorToolImages pulls images out of an error tool result for APIs that
// reject image blocks inside an error result. text keeps the result, with each
// run of images replaced by one placeholder. caption names the tool call and is
// sent with images after every tool result of that turn, in the same user
// message. ok is false when the result has no images to move.
func splitErrorToolImages(m Message) (text, caption string, images []ImageContent, ok bool) {
	if !m.IsError {
		return "", "", nil, false
	}
	if blocks, parsed := toolContentBlocks(m.Content); parsed {
		text, images = replaceToolImageRuns(blocks)
		if len(images) == 0 {
			return "", "", nil, false
		}
		return text, errorToolImageCaption(m), images, true
	}
	var imgs []ImageContent
	for _, img := range m.Images {
		if img.Data == "" {
			continue
		}
		imgs = append(imgs, img)
	}
	if len(imgs) == 0 {
		return "", "", nil, false
	}
	text = m.Content
	if strings.TrimSpace(text) == "" {
		text = errorToolImagePlaceholder
	} else {
		text += "\n" + errorToolImagePlaceholder
	}
	return text, errorToolImageCaption(m), imgs, true
}

func errorToolImageCaption(m Message) string {
	return fmt.Sprintf("Images from the error result of tool call %s (%s):", m.ToolCallID, m.ToolName)
}

func replaceToolImageRuns(blocks []toolContentBlock) (string, []ImageContent) {
	var texts []string
	var imgs []ImageContent
	prevPlaceholder := false
	for _, b := range blocks {
		switch b.Type {
		case "image":
			if b.Data == "" {
				continue
			}
			imgs = append(imgs, ImageContent{Type: "image", Data: b.Data, MimeType: b.MimeType})
			if !prevPlaceholder {
				texts = append(texts, errorToolImagePlaceholder)
				prevPlaceholder = true
			}
		case "text":
			prevPlaceholder = b.Text == errorToolImagePlaceholder
			if b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
	}
	return strings.Join(texts, "\n"), imgs
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
