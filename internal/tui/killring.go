package tui

// killRing is an Emacs-style ring of killed text.
type killRing struct {
	items []string
}

func (k *killRing) push(text string, prepend, accumulate bool) {
	if text == "" {
		return
	}
	if accumulate && len(k.items) > 0 {
		last := k.items[len(k.items)-1]
		k.items = k.items[:len(k.items)-1]
		if prepend {
			k.items = append(k.items, text+last)
		} else {
			k.items = append(k.items, last+text)
		}
		return
	}
	k.items = append(k.items, text)
}

func (k *killRing) peek() string {
	if len(k.items) == 0 {
		return ""
	}
	return k.items[len(k.items)-1]
}

func (k *killRing) rotate() {
	if len(k.items) > 1 {
		last := k.items[len(k.items)-1]
		k.items = append([]string{last}, k.items[:len(k.items)-1]...)
	}
}

func (k *killRing) len() int { return len(k.items) }
