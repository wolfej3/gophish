package controllers

import (
	"net/http"
	"strings"
)

// previewBotAgents are substrings of User-Agent values used by automated
// link-preview renderers (chat/social apps) and email-security URL scanners.
// These clients fetch a URL without a human tapping it; recording their visit
// would create a false "Clicked"/"Opened", and rendering the real page would
// leak the lure into a preview card. Matching is case-insensitive substring.
var previewBotAgents = []string{
	// Link-preview renderers
	"facebookexternalhit", "facebookcatalog", "twitterbot", "slackbot",
	"slack-imgproxy", "discordbot", "telegrambot", "whatsapp", "linkedinbot",
	"pinterest", "redditbot", "applebot", "skypeuripreview", "vkshare",
	"embedly", "quora link preview", "outbrain", "bingpreview", "googlebot",
	"google-structured-data", "developers.google.com/+/web/snippet",
	// Email-security / URL-rewriting scanners
	"proofpoint", "urldefense", "mimecast", "barracuda", "safelinks",
	"forcepoint", "symantec", "trendmicro", "ironport", "cisco",
	"microsoftpreview", "bitdefender", "message labs", "messagelabs",
}

// isPreviewBot reports whether a request looks like an automated link preview
// or URL scanner rather than a human opening the link. It checks the
// User-Agent against previewBotAgents and honors prefetch/preview intent
// headers. An empty User-Agent is treated as human, since some privacy tools
// strip it and we'd rather under-filter than drop a real target.
func isPreviewBot(r *http.Request) bool {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	for _, b := range previewBotAgents {
		if strings.Contains(ua, b) {
			return true
		}
	}
	switch strings.ToLower(r.Header.Get("Purpose")) {
	case "prefetch", "preview":
		return true
	}
	switch strings.ToLower(r.Header.Get("X-Purpose")) {
	case "prefetch", "preview":
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Moz"), "prefetch") {
		return true
	}
	return false
}

// previewPlaceholder is a deliberately empty, boring page served to preview
// bots so no revealing card renders (no Open Graph tags, no lure content).
const previewPlaceholder = "<!doctype html><html><head><title></title></head><body></body></html>"

// servePreviewPlaceholder writes the neutral placeholder response.
func servePreviewPlaceholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(previewPlaceholder))
}
