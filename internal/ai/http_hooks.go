package ai

import (
	"net/http"
)

func extraHeaders(opts Options) map[string]string { return opts.ExtraHeaders }

func applyExtraHeaders(h http.Header, extra map[string]string) {
	for k, v := range extra {
		if v == "" {
			h.Del(k)
			continue
		}
		h.Set(k, v)
	}
}

func transformRequestBody(opts Options, body []byte) []byte {
	if opts.TransformBody == nil {
		return body
	}
	return opts.TransformBody(body)
}

func doHTTP(client *http.Client, req *http.Request, opts Options) (*http.Response, error) {
	applyExtraHeaders(req.Header, extraHeaders(opts))
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err == nil && opts.OnHTTPResponse != nil {
		opts.OnHTTPResponse(resp.StatusCode, resp.Header)
	}
	return resp, err
}
