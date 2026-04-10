package queue

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// URLFetchOptions controls how URL content is fetched.
type URLFetchOptions struct {
	RenderTimeout int
	WaitSelector  string
}

// URLFetcher abstracts URL fetching strategy (static or rendered).
type URLFetcher interface {
	Fetch(ctx context.Context, sourceURL string, options URLFetchOptions) ([]byte, error)
}

// StaticURLFetcher fetches raw HTML via HTTP client.
type StaticURLFetcher struct {
	client *http.Client
}

func NewStaticURLFetcher(client *http.Client) *StaticURLFetcher {
	return &StaticURLFetcher{client: client}
}

func (f *StaticURLFetcher) Fetch(ctx context.Context, sourceURL string, options URLFetchOptions) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ChromedpURLFetcher fetches rendered HTML after executing page scripts.
type ChromedpURLFetcher struct{}

func NewChromedpURLFetcher() *ChromedpURLFetcher {
	return &ChromedpURLFetcher{}
}

func (f *ChromedpURLFetcher) Fetch(ctx context.Context, sourceURL string, options URLFetchOptions) ([]byte, error) {
	renderTimeout := time.Duration(options.RenderTimeout) * time.Second
	if renderTimeout <= 0 {
		renderTimeout = 15 * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	browserCtx, browserCancel := chromedp.NewContext(timeoutCtx)
	defer browserCancel()

	var extractedText string
	actions := []chromedp.Action{
		chromedp.Navigate(sourceURL),
	}
	if options.WaitSelector != "" {
		actions = append(actions, chromedp.WaitVisible(options.WaitSelector, chromedp.ByQuery))
	}
	actions = append(actions, chromedp.Evaluate(`(() => {
  const title = (document.title || "").trim();
  const body = (document.body && document.body.innerText ? document.body.innerText : "").trim();
  if (title && body) return title + "\n\n" + body;
  return body || title || "";
})()`, &extractedText))

	if err := chromedp.Run(browserCtx, actions...); err != nil {
		return nil, err
	}
	if strings.TrimSpace(extractedText) == "" {
		return nil, fmt.Errorf("empty extracted text")
	}
	return []byte(extractedText), nil
}
