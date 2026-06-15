package ncbinucleotide

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// searchResponse is a minimal esearch JSON response for tests.
const searchResponse = `{
  "esearchresult": {
    "count": "2",
    "idlist": ["111", "222"]
  }
}`

// summaryResponse is a minimal esummary JSON response for tests.
const summaryResponse = `{
  "result": {
    "111": {
      "uid": "111",
      "caption": "NC_000001",
      "title": "Homo sapiens chromosome 1",
      "gi": 111,
      "taxid": 9606,
      "slen": 248956422,
      "biomol": "genomic",
      "moltype": "dna",
      "topology": "linear",
      "sourcedb": "refseq",
      "createdate": "2000/09/28",
      "updatedate": "2024/01/01"
    },
    "222": {
      "uid": "222",
      "caption": "NC_000002",
      "title": "Homo sapiens chromosome 2",
      "gi": 222,
      "taxid": 9606,
      "slen": 242193529,
      "biomol": "genomic",
      "moltype": "dna",
      "topology": "linear",
      "sourcedb": "refseq",
      "createdate": "2000/09/28",
      "updatedate": "2024/01/01"
    }
  }
}`

// testServer returns an httptest.Server routing /esearch.fcgi and /esummary.fcgi.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/entrez/eutils/esearch.fcgi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchResponse))
	})
	mux.HandleFunc("/entrez/eutils/esummary.fcgi", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(summaryResponse))
	})
	return httptest.NewServer(mux)
}

// testClient builds a Client wired to srv with no pacing or retries.
func testClient(srv *httptest.Server) *Client {
	return &Client{
		HTTP:      srv.Client(),
		UserAgent: DefaultUserAgent,
		Rate:      0,
		Retries:   0,
	}
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := testClient(srv)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearch(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	orig := BaseURL
	setBaseURL(srv.URL + "/entrez/eutils")
	defer setBaseURL(orig)

	c := testClient(srv)
	ids, count, err := c.Search(context.Background(), "homo sapiens", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if len(ids) != 2 || ids[0] != "111" || ids[1] != "222" {
		t.Errorf("ids = %v, want [111 222]", ids)
	}
}

func TestFetchSequences(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	orig := BaseURL
	setBaseURL(srv.URL + "/entrez/eutils")
	defer setBaseURL(orig)

	c := testClient(srv)
	seqs, err := c.FetchSequences(context.Background(), []string{"111", "222"})
	if err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 2 {
		t.Fatalf("got %d sequences, want 2", len(seqs))
	}
	if seqs[0].ID != "111" || seqs[0].Accession != "NC_000001" {
		t.Errorf("seqs[0] = %+v", seqs[0])
	}
	if seqs[1].ID != "222" || seqs[1].Accession != "NC_000002" {
		t.Errorf("seqs[1] = %+v", seqs[1])
	}
}

func TestGetSequence(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	orig := BaseURL
	setBaseURL(srv.URL + "/entrez/eutils")
	defer setBaseURL(orig)

	c := testClient(srv)
	s, err := c.GetSequence(context.Background(), "111")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "111" {
		t.Errorf("ID = %q, want 111", s.ID)
	}
	if s.TaxID != 9606 {
		t.Errorf("TaxID = %d, want 9606", s.TaxID)
	}
	if s.Length != 248956422 {
		t.Errorf("Length = %d, want 248956422", s.Length)
	}
}

func TestSearchAndFetch(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	orig := BaseURL
	setBaseURL(srv.URL + "/entrez/eutils")
	defer setBaseURL(orig)

	c := testClient(srv)
	seqs, count, err := c.SearchAndFetch(context.Background(), "homo sapiens", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if len(seqs) != 2 {
		t.Fatalf("got %d sequences, want 2", len(seqs))
	}
	if seqs[0].Title != "Homo sapiens chromosome 1" {
		t.Errorf("seqs[0].Title = %q", seqs[0].Title)
	}
}
