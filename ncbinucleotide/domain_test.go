package ncbinucleotide

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network. The client's HTTP behaviour is
// covered in ncbinucleotide_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "ncbinucleotide" {
		t.Errorf("Scheme = %q, want ncbinucleotide", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "ncbinucleotide" {
		t.Errorf("Identity.Binary = %q, want ncbinucleotide", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"3346695951", "sequence", "3346695951"},
		{"NC_000001", "sequence", "NC_000001"},
		{"https://www.ncbi.nlm.nih.gov/nuccore/3346695951", "sequence", "3346695951"},
		{"https://www.ncbi.nlm.nih.gov/nuccore/NC_000001.11", "sequence", "NC_000001.11"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("sequence", "3346695951")
	want := NucleotideURL + "/3346695951"
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "something")
	if err == nil {
		t.Error("Locate with unknown type should return error")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks that Mint,
// Body, and ResolveOn work end to end. The init in domain.go registers the
// domain, so kit.Open finds it.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	s := &Sequence{
		ID:        "3346695951",
		Accession: "NC_140343",
		Title:     "Chelon labrosus genome assembly, chromosome: 7",
	}
	u, err := h.Mint(s)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "ncbinucleotide://sequence/3346695951"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("ncbinucleotide", "12345")
	if err != nil || got.String() != "ncbinucleotide://sequence/12345" {
		t.Errorf("ResolveOn = (%q, %v), want ncbinucleotide://sequence/12345", got.String(), err)
	}
}
