package parser

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type ParsedPage struct {
	URL   string
	Title string
	Text  string
	Links []string
}

func Parse(body io.Reader, baseURL string) ParsedPage {
	doc, err := html.Parse(body)
	if err != nil {
		return ParsedPage{}
	}

	var title string
	var textBuilder strings.Builder
	var links []string

	var f func(*html.Node)
	f = func(n *html.Node) {

		// Skip script/style content
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}

		// Extract title
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = n.FirstChild.Data
		}

		// Extract visible text
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				textBuilder.WriteString(text + " ")
			}
		}

		// Extract links
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					link := resolveURL(baseURL, attr.Val)
					if link != "" {
						links = append(links, link)
					}
				}
			}
		}

		// Traverse children
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}

	f(doc)

	return ParsedPage{
		URL:   baseURL,
		Title: title,
		Text:  textBuilder.String(),
		Links: links,
	}
}

func resolveURL(baseStr, href string) string {
	base, err := url.Parse(baseStr)
	if err != nil {
		return ""
	}

	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}

	return base.ResolveReference(ref).String()
}
