package ai

import (
	"net/http"

	"github.com/Lowpower/pigo/internal/telemetry"
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
	ctx, span := telemetry.Start(req.Context(), "pigo.provider.http")
	defer span.End()
	span.SetAttribute("http.request.method", req.Method)
	if req.URL != nil {
		span.SetAttribute("server.address", req.URL.Host)
	}
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		span.RecordError(err)
		return resp, err
	}
	span.SetAttribute("http.response.status_code", resp.StatusCode)
	if opts.OnHTTPResponse != nil {
		opts.OnHTTPResponse(resp.StatusCode, resp.Header)
	}
	return resp, err
}
