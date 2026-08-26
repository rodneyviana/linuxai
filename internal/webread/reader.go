// Package webread fetches public text pages through an authorization and
// SSRF-resistant network boundary for use as untrusted LLM context.
package webread

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const (
	maxResponseBytes = 1024 * 1024
	maxContentRunes  = 32000
	maxRedirects     = 5
)

var trustedHosts = map[string]bool{
	"kernel.org":               true,
	"www.kernel.org":           true,
	"docs.kernel.org":          true,
	"gnu.org":                  true,
	"www.gnu.org":              true,
	"freedesktop.org":          true,
	"www.freedesktop.org":      true,
	"man7.org":                 true,
	"www.man7.org":             true,
	"www.debian.org":           true,
	"manpages.debian.org":      true,
	"wiki.debian.org":          true,
	"ubuntu.com":               true,
	"help.ubuntu.com":          true,
	"documentation.ubuntu.com": true,
	"fedoraproject.org":        true,
	"docs.fedoraproject.org":   true,
	"redhat.com":               true,
	"access.redhat.com":        true,
	"docs.redhat.com":          true,
	"archlinux.org":            true,
	"wiki.archlinux.org":       true,
	"alpinelinux.org":          true,
	"wiki.alpinelinux.org":     true,
	"opensuse.org":             true,
	"doc.opensuse.org":         true,
	"suse.com":                 true,
	"documentation.suse.com":   true,
	"go.dev":                   true,
	"pkg.go.dev":               true,
	"python.org":               true,
	"docs.python.org":          true,
	"rust-lang.org":            true,
	"doc.rust-lang.org":        true,
	"docs.rs":                  true,
	"developer.mozilla.org":    true,
}

// Authorizer is called before fetching an origin outside the built-in
// whitelist. The implementation may cache session approvals by origin.
type Authorizer func(origin, rawURL string) bool

// Page is the bounded, readable representation returned to the model.
type Page struct {
	Title       string `json:"title,omitempty"`
	URL         string `json:"url"`
	Kind        string `json:"kind,omitempty"`
	Links       []Link `json:"links,omitempty"`
	RetrievedAt string `json:"retrieved_at"`
	Content     string `json:"content"`
}

// Link is a bounded, resolved hyperlink discovered in readable page content.
type Link struct {
	Text string `json:"text,omitempty"`
	URL  string `json:"url"`
}

// Reader fetches public text pages. Its private hooks exist only so package
// tests can use httptest servers without weakening production validation.
type Reader struct {
	authorize    Authorizer
	resolver     *net.Resolver
	allowPrivate bool
}

// New creates a guarded reader. A nil authorizer denies non-whitelisted
// origins.
func New(authorize Authorizer) *Reader {
	return &Reader{authorize: authorize, resolver: net.DefaultResolver}
}

// TrustedURL reports whether a URL belongs to a curated documentation origin.
func TrustedURL(rawURL string) bool {
	parsed, err := parseURL(rawURL)
	return err == nil && trustedParsedURL(parsed)
}

// Origin returns the normalized scheme and authority used for authorization.
func Origin(rawURL string) (string, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return "", err
	}
	return origin(parsed), nil
}

// Read fetches and extracts one public text page. Page content is untrusted
// data and must never be interpreted as application instructions.
func (r *Reader) Read(ctx context.Context, rawURL string) (Page, error) {
	parsed, err := parseURL(rawURL)
	if err != nil {
		return Page{}, err
	}
	if err := r.authorizeURL(parsed); err != nil {
		return Page{}, err
	}

	redirects := 0
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           r.safeDialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirects++
			if redirects > maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			if err := validateParsedURL(req.URL); err != nil {
				return err
			}
			if len(via) > 0 && origin(req.URL) != origin(via[len(via)-1].URL) {
				return r.authorizeURL(req.URL)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Page{}, fmt.Errorf("building web request: %w", err)
	}
	req.Header.Set("User-Agent", "linuxai/1 web reader")
	req.Header.Set("Accept", "text/html, text/plain, text/markdown, application/xhtml+xml, application/rss+xml, application/atom+xml, application/xml, text/xml, application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("reading %s: %w", parsed.Hostname(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Page{}, fmt.Errorf("site returned %s", resp.Status)
	}

	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !textMediaType(mediaType) {
		return Page{}, fmt.Errorf("unsupported content type %q", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Page{}, fmt.Errorf("reading response body: %w", err)
	}
	if len(body) > maxResponseBytes {
		return Page{}, fmt.Errorf("page exceeds %d byte limit", maxResponseBytes)
	}

	title := ""
	content := string(body)
	kind := "page"
	var links []Link
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		title, content, links, kind, err = extractHTML(body, resp.Request.URL)
		if err != nil {
			return Page{}, err
		}
	} else if feedMediaType(mediaType) {
		title, content, err = extractFeed(body)
		if err != nil {
			return Page{}, err
		}
		kind = "feed"
	} else {
		content = strings.Join(strings.Fields(content), " ")
	}
	content = truncateRunes(content, maxContentRunes)
	if strings.TrimSpace(content) == "" {
		return Page{}, fmt.Errorf("page contains no readable text")
	}

	return Page{
		Title:       title,
		URL:         resp.Request.URL.String(),
		Kind:        kind,
		Links:       links,
		RetrievedAt: time.Now().UTC().Format(time.RFC3339),
		Content:     content,
	}, nil
}

func (r *Reader) authorizeURL(parsed *url.URL) error {
	if trustedParsedURL(parsed) {
		return nil
	}
	if r.authorize == nil || !r.authorize(origin(parsed), parsed.String()) {
		return fmt.Errorf("access to %s was not authorized", origin(parsed))
	}
	return nil
}

func parseURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if err := validateParsedURL(parsed); err != nil {
		return nil, err
	}
	parsed.Fragment = ""
	return parsed, nil
}

func validateParsedURL(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are supported")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("URL has no host")
	}
	if parsed.User != nil {
		return fmt.Errorf("URLs containing credentials are not allowed")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid URL port")
		}
	}
	return nil
}

func origin(parsed *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if parsed.Port() != "" {
		host += ":" + parsed.Port()
	}
	return strings.ToLower(parsed.Scheme) + "://" + host
}

func trustedParsedURL(parsed *url.URL) bool {
	if parsed.Scheme != "https" {
		return false
	}
	port := parsed.Port()
	if port != "" && port != "443" {
		return false
	}
	host := parsed.Hostname()
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return trustedHosts[host] || host == "wikipedia.org" || strings.HasSuffix(host, ".wikipedia.org")
}

func (r *Reader) safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid network address: %w", err)
	}
	addresses, err := r.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolving host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("host resolved to no addresses")
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, address := range addresses {
		if !r.allowPrivate && !publicIP(address.IP) {
			return nil, fmt.Errorf("refusing non-public address %s", address.IP)
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("connecting to host: %w", lastErr)
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func textMediaType(mediaType string) bool {
	switch strings.ToLower(mediaType) {
	case "text/html", "application/xhtml+xml", "text/plain", "text/markdown", "application/json", "application/rss+xml", "application/atom+xml", "application/xml", "text/xml":
		return true
	default:
		return false
	}
}

func feedMediaType(mediaType string) bool {
	switch strings.ToLower(mediaType) {
	case "application/rss+xml", "application/atom+xml", "application/xml", "text/xml":
		return true
	default:
		return false
	}
}

type feedDocument struct {
	Title   string      `xml:"title"`
	Channel feedChannel `xml:"channel"`
	Entries []feedEntry `xml:"entry"`
}

type feedChannel struct {
	Title string     `xml:"title"`
	Items []feedItem `xml:"item"`
}

type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Published   string `xml:"pubDate"`
}

type feedEntry struct {
	Title     string     `xml:"title"`
	Links     []feedLink `xml:"link"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
}

type feedLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func extractFeed(body []byte) (string, string, error) {
	var document feedDocument
	if err := xml.Unmarshal(body, &document); err != nil {
		return "", "", fmt.Errorf("parsing web feed: %w", err)
	}
	title := strings.TrimSpace(document.Channel.Title)
	if title == "" {
		title = strings.TrimSpace(document.Title)
	}

	var content strings.Builder
	const maxEntries = 20
	for index, item := range document.Channel.Items {
		if index >= maxEntries {
			break
		}
		writeFeedEntry(&content, item.Title, item.Link, item.Published, item.Description)
	}
	for index, entry := range document.Entries {
		if index >= maxEntries {
			break
		}
		link := ""
		for _, candidate := range entry.Links {
			if candidate.Rel == "" || candidate.Rel == "alternate" {
				link = candidate.Href
				break
			}
		}
		published := entry.Published
		if published == "" {
			published = entry.Updated
		}
		summary := entry.Summary
		if summary == "" {
			summary = entry.Content
		}
		writeFeedEntry(&content, entry.Title, link, published, summary)
	}
	if content.Len() == 0 {
		return "", "", fmt.Errorf("web feed contains no entries")
	}
	return title, strings.TrimSpace(content.String()), nil
}

func writeFeedEntry(output *strings.Builder, title, link, published, summary string) {
	title = strings.Join(strings.Fields(title), " ")
	link = strings.TrimSpace(link)
	published = strings.Join(strings.Fields(published), " ")
	summary = stripMarkup(summary)
	fmt.Fprintf(output, "Title: %s\nURL: %s\n", title, link)
	if published != "" {
		fmt.Fprintf(output, "Published: %s\n", published)
	}
	if summary != "" {
		fmt.Fprintf(output, "Summary: %s\n", summary)
	}
	output.WriteByte('\n')
}

func stripMarkup(text string) string {
	if !strings.Contains(text, "<") {
		return strings.Join(strings.Fields(text), " ")
	}
	_, content, _, _, err := extractHTML([]byte("<html><body>"+text+"</body></html>"), nil)
	if err != nil {
		return strings.Join(strings.Fields(text), " ")
	}
	return content
}

func extractHTML(body []byte, baseURL *url.URL) (string, string, []Link, string, error) {
	document, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", "", nil, "", fmt.Errorf("parsing HTML: %w", err)
	}
	title := nodeText(findElement(document, "title"))
	kind := classifyHTML(document, baseURL)
	root := findElement(document, "main")
	if root == nil {
		root = findElement(document, "article")
	}
	if root == nil {
		root = findElement(document, "body")
	}
	if root == nil {
		root = document
	}
	return strings.TrimSpace(title), strings.Join(strings.Fields(nodeText(root)), " "), collectLinks(root, baseURL), kind, nil
}

func classifyHTML(document *html.Node, pageURL *url.URL) string {
	if findElement(document, "article") != nil || hasMetaValue(document, "property", "og:type", "article") {
		return "article"
	}
	if pageURL == nil {
		return "page"
	}
	path := strings.ToLower(strings.Trim(pageURL.Path, "/"))
	if path == "" {
		return "index"
	}
	if strings.HasPrefix(path, "articles/") && path != "articles/rss" {
		return "article"
	}
	last := path
	if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
		last = path[slash+1:]
	}
	switch last {
	case "kernel", "news", "articles", "archive", "archives", "category", "search":
		return "index"
	default:
		return "page"
	}
}

func hasMetaValue(node *html.Node, key, name, value string) bool {
	if node == nil {
		return false
	}
	if node.Type == html.ElementNode && node.Data == "meta" {
		matched := false
		content := ""
		for _, attribute := range node.Attr {
			if attribute.Key == key && strings.EqualFold(attribute.Val, name) {
				matched = true
			}
			if attribute.Key == "content" {
				content = attribute.Val
			}
		}
		if matched && strings.EqualFold(strings.TrimSpace(content), value) {
			return true
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if hasMetaValue(child, key, name, value) {
			return true
		}
	}
	return false
}

func collectLinks(root *html.Node, baseURL *url.URL) []Link {
	if root == nil || baseURL == nil {
		return nil
	}
	const maxLinks = 80
	seen := make(map[string]bool)
	links := make([]Link, 0, maxLinks)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || len(links) >= maxLinks {
			return
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "noscript", "svg", "nav", "header", "footer", "form":
				return
			case "a":
				href := ""
				for _, attribute := range node.Attr {
					if attribute.Key == "href" {
						href = strings.TrimSpace(attribute.Val)
						break
					}
				}
				if resolved := resolveLink(baseURL, href); resolved != "" && !seen[resolved] {
					seen[resolved] = true
					text := strings.Join(strings.Fields(nodeText(node)), " ")
					textRunes := []rune(text)
					if len(textRunes) > 200 {
						text = string(textRunes[:200]) + "…"
					}
					links = append(links, Link{Text: text, URL: resolved})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return links
}

func resolveLink(baseURL *url.URL, href string) string {
	if href == "" {
		return ""
	}
	reference, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := baseURL.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	if resolved.Hostname() == "" || resolved.User != nil {
		return ""
	}
	resolved.Fragment = ""
	return resolved.String()
}

func findElement(node *html.Node, name string) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "noscript", "svg", "nav", "header", "footer", "form":
			return ""
		}
	}
	if node.Type == html.TextNode {
		return node.Data + " "
	}
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(nodeText(child))
	}
	return text.String()
}

func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
