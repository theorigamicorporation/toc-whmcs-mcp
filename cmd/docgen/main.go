// Command docgen generates internal/registry/actions_gen.go from the published
// WHMCS API reference.
//
// It runs at development time only. The built server never contacts
// developers.whmcs.com; it uses the committed generated file. That keeps the
// runtime free of a scraping dependency and makes every schema change show up
// as a reviewable diff.
//
//	just gen          regenerate the registry
//	just gen-check    fail if the committed registry is stale
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/theorigamicorporation/toc-whmcs-mcp/internal/registry"
)

const (
	defaultIndexURL = "https://developers.whmcs.com/api/api-index/"
	defaultOutput   = "internal/registry/actions_gen.go"
	userAgent       = "toc-whmcs-mcp-docgen/1.0 (internal tooling)"
	// maxPageBytes bounds each documentation page. The largest real page is
	// well under 1 MB.
	maxPageBytes = 4 << 20
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "docgen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		indexURL    = flag.String("index", defaultIndexURL, "URL of the WHMCS API index page")
		output      = flag.String("out", defaultOutput, "path of the generated Go file")
		concurrency = flag.Int("concurrency", 4, "concurrent documentation fetches")
		delay       = flag.Duration("delay", 150*time.Millisecond, "pause between fetches per worker")
		timeout     = flag.Duration("timeout", 5*time.Minute, "overall deadline")
		check       = flag.Bool("check", false, "do not write; exit non-zero if the committed file is stale")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}

	fmt.Fprintf(os.Stderr, "fetching index %s\n", *indexURL)
	body, err := fetch(ctx, client, *indexURL)
	if err != nil {
		return fmt.Errorf("fetch index: %w", err)
	}
	entries, err := parseIndex(strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	entries = dedupe(entries)
	fmt.Fprintf(os.Stderr, "found %d actions across %d categories\n", len(entries), countCategories(entries))

	actions, err := fetchAll(ctx, client, entries, *concurrency, *delay)
	if err != nil {
		return err
	}

	if err := verifyClassification(actions); err != nil {
		return err
	}

	src, err := emit(actions)
	if err != nil {
		return err
	}

	if *check {
		existing, err := os.ReadFile(*output)
		if err != nil {
			return fmt.Errorf("read %s for comparison: %w", *output, err)
		}
		if string(existing) != string(src) {
			return fmt.Errorf("%s is stale; run `just gen` and commit the result", *output)
		}
		fmt.Fprintln(os.Stderr, "registry is up to date")
		return nil
	}

	if err := os.WriteFile(*output, src, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", *output, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d actions)\n", *output, len(actions))
	return nil
}

// fetchAll retrieves every action page, bounded in concurrency and paced by a
// per-worker delay so that regenerating the registry is not a burst of 160
// requests at the vendor's documentation site.
func fetchAll(ctx context.Context, client *http.Client, entries []indexEntry, concurrency int, delay time.Duration) ([]registry.Action, error) {
	if concurrency < 1 {
		concurrency = 1
	}

	type result struct {
		action registry.Action
		err    error
	}

	jobs := make(chan indexEntry)
	results := make(chan result)

	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				a, err := fetchAction(ctx, client, e)
				select {
				case results <- result{action: a, err: err}:
				case <-ctx.Done():
					return
				}
				time.Sleep(delay)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, e := range entries {
			select {
			case jobs <- e:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		actions []registry.Action
		errs    []error
		done    int
	)
	for r := range results {
		done++
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		actions = append(actions, r.action)
		if done%20 == 0 {
			fmt.Fprintf(os.Stderr, "  %d/%d\n", done, len(entries))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%d action pages failed: %w", len(errs), errors.Join(errs...))
	}
	return actions, nil
}

func fetchAction(ctx context.Context, client *http.Client, e indexEntry) (registry.Action, error) {
	url := "https://developers.whmcs.com/api-reference/" + e.Slug + "/"
	body, err := fetch(ctx, client, url)
	if err != nil {
		return registry.Action{}, fmt.Errorf("%s: %w", e.Name, err)
	}
	page, err := parseAction(e.Name, strings.NewReader(string(body)))
	if err != nil {
		return registry.Action{}, fmt.Errorf("%s: %w", e.Name, err)
	}
	return registry.Action{
		Name:     e.Name,
		Category: e.Category,
		Slug:     e.Slug,
		Summary:  page.Summary,
		Params:   page.Params,
		Response: page.Response,
	}, nil
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
}

// verifyClassification is the safety gate on generation.
//
// If the vendor adds an action, generation fails rather than silently producing
// a registry entry that Classify would default to write. A human has to look at
// the new action and decide what it does. The reverse direction is a warning,
// not a failure: an action disappearing from the documentation does not make
// the classification wrong.
func verifyClassification(actions []registry.Action) error {
	var unclassified []string
	seen := make(map[string]bool, len(actions))
	for _, a := range actions {
		seen[strings.ToLower(a.Name)] = true
		if !registry.Classified(a.Name) {
			unclassified = append(unclassified, a.Name)
		}
	}
	sort.Strings(unclassified)

	var stale []string
	for _, name := range registry.ClassifiedNames() {
		if !seen[strings.ToLower(name)] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: %d classified actions are no longer in the vendor index: %s\n",
			len(stale), strings.Join(stale, ", "))
	}

	if len(unclassified) > 0 {
		return fmt.Errorf(
			"%d action(s) have no safety classification: %s\n"+
				"Add each to the classification table in internal/registry/classification.go.\n"+
				"Classify conservatively: anything that moves money, changes provisioning,\n"+
				"alters global configuration, or mails a customer is destructive",
			len(unclassified), strings.Join(unclassified, ", "))
	}
	return nil
}

func dedupe(entries []indexEntry) []indexEntry {
	seen := make(map[string]bool, len(entries))
	out := entries[:0]
	for _, e := range entries {
		key := strings.ToLower(e.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func countCategories(entries []indexEntry) int {
	seen := make(map[string]bool)
	for _, e := range entries {
		seen[e.Category] = true
	}
	return len(seen)
}
