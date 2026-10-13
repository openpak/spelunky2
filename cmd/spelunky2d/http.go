package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"openpak/spelunky2/redact"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeOctets answers the binary protocol: application/octet-stream, no JSON.
func writeOctets(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}

// shortErr renders an identity failure for the log without its URL: the
// nx-baas address is deployment detail, and the token never reaches a log.
func shortErr(err error) string {
	if err == nil {
		return "empty token"
	}
	if errors.Is(err, errTokenRejected) {
		return "token rejected"
	}
	if errors.Is(err, errAccountBanned) {
		return "account banned"
	}
	return "identity lookup failed"
}

// sessionTag names a session in a log line without printing it: the session
// is a credential for the later calls.
func sessionTag(sess [16]byte) string {
	return redact.Tag(sess[:])
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
