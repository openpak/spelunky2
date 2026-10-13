package main

import (
	"encoding/json"
	"net/http"
	"net/url"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// logURL renders a request URL for the log: the path scrubbed and cut, the
// query parameters with secret values hidden. The join code and the Nintendo
// token may both ride the query string, so it is never logged raw.
func logURL(u *url.URL) string {
	if u == nil {
		return "-"
	}
	if u.RawQuery == "" {
		return logPath(u.Path)
	}
	vals, _ := url.ParseQuery(u.RawQuery)
	return logPath(u.Path) + "?" + redactQuery(vals)
}
