// Package ncbinucleotide is the library behind the ncbinucleotide command line:
// the HTTP client, request shaping, and the typed data models for NCBI Nucleotide.
//
// The Client talks to the NCBI eUtils API (db=nucleotide). It sets a real
// User-Agent, paces requests to stay within NCBI's rate limits, and retries
// transient failures (429 and 5xx) automatically.
package ncbinucleotide

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to NCBI. A real, honest User-Agent is
// both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "ncbinucleotide/dev (+https://github.com/tamnd/ncbinucleotide-cli)"

// Host is the NCBI eUtils hostname this client talks to.
const Host = "eutils.ncbi.nlm.nih.gov"

// BaseURL is the root every eUtils request is built from. It is a var so
// tests can swap it to point at an httptest.Server.
var BaseURL = "https://" + Host + "/entrez/eutils"

// NucleotideURL is the base URL for NCBI Nucleotide human-readable pages.
const NucleotideURL = "https://www.ncbi.nlm.nih.gov/nuccore"

// setBaseURL replaces BaseURL; used only by package tests.
func setBaseURL(u string) { BaseURL = u }

// Default client settings.
const (
	Rate    = 400 * time.Millisecond
	Retries = 3
	Timeout = 30 * time.Second
)

// --- wire types ---

type wireSearch struct {
	ESearchResult struct {
		Count  string   `json:"count"`
		IDList []string `json:"idlist"`
	} `json:"esearchresult"`
}

type wireSequence struct {
	UID        string `json:"uid"`
	Caption    string `json:"caption"`
	Title      string `json:"title"`
	Extra      string `json:"extra"`
	GI         int    `json:"gi"`
	CreateDate string `json:"createdate"`
	UpdateDate string `json:"updatedate"`
	TaxID      int    `json:"taxid"`
	Length     int    `json:"slen"`
	BioMol     string `json:"biomol"`
	MolType    string `json:"moltype"`
	Topology   string `json:"topology"`
	SourceDB   string `json:"sourcedb"`
	ProjectID  int    `json:"projectid"`
}

type wireSummary struct {
	Result map[string]json.RawMessage `json:"result"`
}

// --- public types ---

// Sequence is a single NCBI Nucleotide record.
type Sequence struct {
	ID         string `json:"id"          kit:"id"`
	Accession  string `json:"accession"`
	Title      string `json:"title"`
	TaxID      int    `json:"tax_id,omitempty"`
	Length     int    `json:"length,omitempty"`
	BioMol     string `json:"biomol,omitempty"`
	MolType    string `json:"mol_type,omitempty"`
	Topology   string `json:"topology,omitempty"`
	SourceDB   string `json:"source_db,omitempty"`
	CreateDate string `json:"create_date,omitempty"`
	UpdateDate string `json:"update_date,omitempty"`
}

func sequenceFromWire(w *wireSequence) *Sequence {
	return &Sequence{
		ID:         w.UID,
		Accession:  w.Caption,
		Title:      w.Title,
		TaxID:      w.TaxID,
		Length:     w.Length,
		BioMol:     w.BioMol,
		MolType:    w.MolType,
		Topology:   w.Topology,
		SourceDB:   w.SourceDB,
		CreateDate: w.CreateDate,
		UpdateDate: w.UpdateDate,
	}
}

// --- client ---

// Client talks to NCBI eUtils over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults aligned with NCBI's
// recommended usage: a 30s timeout, 400ms minimum gap between requests
// (3 requests/s without an API key), and 3 retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: Timeout},
		UserAgent: DefaultUserAgent,
		Rate:      Rate,
		Retries:   Retries,
	}
}

// Search queries NCBI Nucleotide for sequences matching query and returns
// the list of UIDs plus the total count of matching records.
func (c *Client) Search(ctx context.Context, query string, limit, start int) ([]string, int, error) {
	if limit <= 0 {
		limit = 20
	}
	u := BaseURL + "/esearch.fcgi?" + url.Values{
		"db":       {"nucleotide"},
		"term":     {query},
		"retmax":   {fmt.Sprintf("%d", limit)},
		"retstart": {fmt.Sprintf("%d", start)},
		"retmode":  {"json"},
	}.Encode()

	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, 0, fmt.Errorf("search %q: %w", query, err)
	}

	var ws wireSearch
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, 0, fmt.Errorf("search %q: decode: %w", query, err)
	}

	var count int
	fmt.Sscanf(ws.ESearchResult.Count, "%d", &count)
	return ws.ESearchResult.IDList, count, nil
}

// FetchSequences retrieves summaries for a list of UIDs in a single batch
// request and returns them as Sequence records.
func (c *Client) FetchSequences(ctx context.Context, ids []string) ([]*Sequence, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	u := BaseURL + "/esummary.fcgi?" + url.Values{
		"db":      {"nucleotide"},
		"id":      {strings.Join(ids, ",")},
		"retmode": {"json"},
	}.Encode()

	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch sequences: %w", err)
	}

	var ws wireSummary
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, fmt.Errorf("fetch sequences: decode: %w", err)
	}

	out := make([]*Sequence, 0, len(ids))
	for _, id := range ids {
		raw, ok := ws.Result[id]
		if !ok {
			continue
		}
		var wseq wireSequence
		if err := json.Unmarshal(raw, &wseq); err != nil {
			continue
		}
		out = append(out, sequenceFromWire(&wseq))
	}
	return out, nil
}

// GetSequence fetches a single sequence by its numeric UID (GI number).
func (c *Client) GetSequence(ctx context.Context, uid string) (*Sequence, error) {
	seqs, err := c.FetchSequences(ctx, []string{uid})
	if err != nil {
		return nil, err
	}
	if len(seqs) == 0 {
		return nil, fmt.Errorf("sequence %s: not found", uid)
	}
	return seqs[0], nil
}

// SearchAndFetch searches for sequences and returns them with the total count
// in a single call.
func (c *Client) SearchAndFetch(ctx context.Context, query string, limit, start int) ([]*Sequence, int, error) {
	ids, count, err := c.Search(ctx, query, limit, start)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return nil, count, nil
	}
	seqs, err := c.FetchSequences(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	return seqs, count, nil
}

// Get fetches url and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
