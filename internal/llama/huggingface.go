package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const huggingFaceAPI = "https://huggingface.co"

var (
	quantizationPattern = regexp.MustCompile(`(?i)(?:^|[-_.])((?:UD-)?(?:IQ\d(?:_[A-Z0-9]+)+|Q\d(?:_[A-Z0-9]+)+|BF16|F16|F32|MXFP\d(?:_[A-Z0-9]+)*))$`)
	shardSuffixPattern  = regexp.MustCompile(`-\d{5}-of-\d{5}$`)
	hfExactModel        = regexp.MustCompile(`^[^/\s]+/[^:\s]+(?::[^\s:]+)?$`)
)

// HFModel is one Hugging Face GGUF search hit.
type HFModel struct {
	ID        string `json:"id"`
	Downloads int    `json:"downloads"`
}

// HFQuantization is one GGUF quantization on a repo.
type HFQuantization struct {
	Name string
	Size *int64
}

// HFDetails is GET /api/models/{id}?blobs=true.
type HFDetails struct {
	ID            string
	Gated         string // "", "auto", or "manual"
	Quantizations []HFQuantization
}

// FindHuggingFaceToken returns HF_TOKEN or a cached huggingface token file.
func FindHuggingFaceToken() string {
	if v := strings.TrimSpace(os.Getenv("HF_TOKEN")); v != "" {
		return v
	}
	var paths []string
	if p := strings.TrimSpace(os.Getenv("HF_TOKEN_PATH")); p != "" {
		paths = append(paths, p)
	}
	if home := strings.TrimSpace(os.Getenv("HF_HOME")); home != "" {
		paths = append(paths, filepath.Join(home, "token"))
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "huggingface", "token"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".cache", "huggingface", "token"))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	return ""
}

func hfGET(ctx context.Context, path, token, baseURL string) ([]byte, *http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = huggingFaceAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	payload, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("hugging face returned HTTP %d", resp.StatusCode)
		if resp.StatusCode == 429 {
			delay := resp.Header.Get("Retry-After")
			if delay == "" {
				if m := regexp.MustCompile(`(?:^|;)t=(\d+)`).FindStringSubmatch(resp.Header.Get("Ratelimit")); len(m) == 2 {
					delay = m[1]
				}
			}
			if delay != "" {
				msg = "hugging face rate limit reached; retry in " + delay + "s"
			} else {
				msg = "hugging face rate limit reached"
			}
		}
		var wrap struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &wrap) == nil && wrap.Error != "" {
			msg = wrap.Error
		}
		return nil, resp, fmt.Errorf("%s", msg)
	}
	return payload, resp, nil
}

// SearchHuggingFace lists GGUF models matching query, sorted by downloads.
func SearchHuggingFace(ctx context.Context, query, token, baseURL string) ([]HFModel, error) {
	params := url.Values{
		"search":    {query},
		"filter":    {"gguf"},
		"sort":      {"downloads"},
		"direction": {"-1"},
		"limit":     {"20"},
	}
	payload, _, err := hfGET(ctx, "/api/models?"+params.Encode(), token, baseURL)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID        string `json:"id"`
		Downloads int    `json:"downloads"`
	}
	if json.Unmarshal(payload, &raw) != nil {
		return nil, fmt.Errorf("hugging face returned invalid search results")
	}
	out := make([]HFModel, 0, len(raw))
	for _, m := range raw {
		if m.ID == "" {
			continue
		}
		out = append(out, HFModel{ID: m.ID, Downloads: m.Downloads})
	}
	return out, nil
}

// HuggingFaceDetails loads repo metadata and GGUF quantizations.
func HuggingFaceDetails(ctx context.Context, id, token, baseURL string) (HFDetails, error) {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	payload, _, err := hfGET(ctx, "/api/models/"+strings.Join(parts, "/")+"?blobs=true", token, baseURL)
	if err != nil {
		return HFDetails{}, err
	}
	var raw struct {
		ID       string `json:"id"`
		Gated    any    `json:"gated"`
		Siblings []struct {
			Rfilename string `json:"rfilename"`
			Size      *int64 `json:"size"`
		} `json:"siblings"`
	}
	if json.Unmarshal(payload, &raw) != nil {
		return HFDetails{}, fmt.Errorf("hugging face returned invalid model details")
	}
	out := HFDetails{ID: raw.ID}
	if out.ID == "" {
		out.ID = id
	}
	switch v := raw.Gated.(type) {
	case string:
		if v == "auto" || v == "manual" {
			out.Gated = v
		}
	}
	type sizeAcc struct {
		total    int64
		complete bool
	}
	sizes := map[string]*sizeAcc{}
	for _, file := range raw.Siblings {
		name := file.Rfilename
		if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
			continue
		}
		base := name
		if i := strings.LastIndex(name, "/"); i >= 0 {
			base = name[i+1:]
		}
		if strings.HasPrefix(strings.ToLower(base), "mmproj") {
			continue
		}
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		stem = shardSuffixPattern.ReplaceAllString(stem, "")
		m := quantizationPattern.FindStringSubmatch(stem)
		if len(m) < 2 {
			continue
		}
		quant := strings.ToUpper(m[1])
		cur := sizes[quant]
		if cur == nil {
			cur = &sizeAcc{complete: true}
			sizes[quant] = cur
		}
		if file.Size != nil {
			cur.total += *file.Size
		} else {
			cur.complete = false
		}
	}
	for name, acc := range sizes {
		q := HFQuantization{Name: name}
		if acc.complete {
			n := acc.total
			q.Size = &n
		}
		out.Quantizations = append(out.Quantizations, q)
	}
	sort.Slice(out.Quantizations, func(i, j int) bool {
		a, b := out.Quantizations[i], out.Quantizations[j]
		if a.Name == "Q4_K_M" {
			return true
		}
		if b.Name == "Q4_K_M" {
			return false
		}
		as, bs := int64(1<<62), int64(1<<62)
		if a.Size != nil {
			as = *a.Size
		}
		if b.Size != nil {
			bs = *b.Size
		}
		if as != bs {
			return as < bs
		}
		return a.Name < b.Name
	})
	return out, nil
}

// ParseHuggingFaceModel splits owner/repo[:quant].
func ParseHuggingFaceModel(value string) (repository, quantization string) {
	slash := strings.Index(value, "/")
	if slash < 0 {
		return value, ""
	}
	rest := value[slash+1:]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return value, ""
	}
	return value[:slash+1+colon], rest[colon+1:]
}

// ExactHuggingFaceID reports whether query is owner/repo[:quant].
func ExactHuggingFaceID(query string) bool {
	return hfExactModel.MatchString(strings.TrimSpace(query))
}

// FormatSearch is the /llama search listing.
func FormatSearch(models []HFModel) string {
	var b strings.Builder
	b.WriteString("Hugging Face GGUF models\n")
	if len(models) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for _, m := range models {
		fmt.Fprintf(&b, "  %s  %d downloads\n", m.ID, m.Downloads)
	}
	b.WriteString("Download with /llama download owner/repo:QUANT\n")
	return b.String()
}

// FormatBytes is a short IEC byte size.
func FormatBytes(bytes float64) string {
	if bytes < 1024 {
		return strconv.FormatInt(int64(bytes), 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := bytes / 1024
	unit := units[0]
	for i := 1; i < len(units) && value >= 1024; i++ {
		value /= 1024
		unit = units[i]
	}
	if value >= 10 {
		return strconv.FormatFloat(value, 'f', 1, 64) + " " + unit
	}
	return strconv.FormatFloat(value, 'f', 2, 64) + " " + unit
}

// CompactCount is a short downloads/count label.
func CompactCount(n int) string {
	if n >= 1_000_000 {
		v := float64(n) / 1_000_000
		if n >= 10_000_000 {
			return strconv.FormatFloat(v, 'f', 0, 64) + "M"
		}
		return strconv.FormatFloat(v, 'f', 1, 64) + "M"
	}
	if n >= 1_000 {
		v := float64(n) / 1_000
		if n >= 100_000 {
			return strconv.FormatFloat(v, 'f', 0, 64) + "k"
		}
		return strconv.FormatFloat(v, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(n)
}
