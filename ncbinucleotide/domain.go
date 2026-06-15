package ncbinucleotide

import (
	"context"
	"net/url"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes ncbinucleotide as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/ncbinucleotide-cli/ncbinucleotide"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// ncbinucleotide:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone ncbinucleotide binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the ncbinucleotide driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "ncbinucleotide",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "ncbinucleotide",
			Short:  "Browse NCBI Nucleotide sequences from the command line.",
			Long: `Browse NCBI Nucleotide sequences from the command line.

ncbinucleotide reads public NCBI Nucleotide data over plain HTTPS via the
eUtils API, shapes it into clean records, and prints output that pipes into
the rest of your tools. No API key needed.`,
			Site: Host,
			Repo: "https://github.com/tamnd/ncbinucleotide-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		Summary: "Search sequences by keyword",
		Args:    []kit.Arg{{Name: "query", Help: "search query"}},
	}, searchOp)

	kit.Handle(app, kit.OpMeta{
		Name:     "sequence",
		Group:    "read",
		Single:   true,
		Summary:  "Fetch a sequence by numeric GI/UID",
		URIType:  "sequence",
		Resolver: true,
		Args:     []kit.Arg{{Name: "uid", Help: "numeric GI / UID"}},
	}, sequenceOp)

	kit.Handle(app, kit.OpMeta{
		Name:    "organism",
		Group:   "read",
		List:    true,
		Summary: "List sequences from an organism",
		Args:    []kit.Arg{{Name: "name", Help: "organism name"}},
	}, organismOp)

	kit.Handle(app, kit.OpMeta{
		Name:    "gene",
		Group:   "read",
		List:    true,
		Summary: "List sequences for a gene",
		Args:    []kit.Arg{{Name: "name", Help: "gene name"}},
	}, geneOp)
}

// newClient builds the Client from host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type searchInput struct {
	Query  string  `kit:"arg" help:"search query"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Start  int     `kit:"flag" help:"result offset"`
	Client *Client `kit:"inject"`
}

type sequenceInput struct {
	UID    string  `kit:"arg" help:"numeric GI / UID"`
	Client *Client `kit:"inject"`
}

type organismInput struct {
	Name   string  `kit:"arg" help:"organism name"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Start  int     `kit:"flag" help:"result offset"`
	Client *Client `kit:"inject"`
}

type geneInput struct {
	Name   string  `kit:"arg" help:"gene name"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Start  int     `kit:"flag" help:"result offset"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func searchOp(ctx context.Context, in searchInput, emit func(*Sequence) error) error {
	seqs, _, err := in.Client.SearchAndFetch(ctx, in.Query, in.Limit, in.Start)
	if err != nil {
		return mapErr(err)
	}
	for _, s := range seqs {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

func sequenceOp(ctx context.Context, in sequenceInput, emit func(*Sequence) error) error {
	uid := strings.TrimSpace(in.UID)
	s, err := in.Client.GetSequence(ctx, uid)
	if err != nil {
		return mapErr(err)
	}
	return emit(s)
}

func organismOp(ctx context.Context, in organismInput, emit func(*Sequence) error) error {
	query := in.Name + "[organism]"
	seqs, _, err := in.Client.SearchAndFetch(ctx, query, in.Limit, in.Start)
	if err != nil {
		return mapErr(err)
	}
	for _, s := range seqs {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

func geneOp(ctx context.Context, in geneInput, emit func(*Sequence) error) error {
	query := in.Name + "[gene name]"
	seqs, _, err := in.Client.SearchAndFetch(ctx, query, in.Limit, in.Start)
	if err != nil {
		return mapErr(err)
	}
	for _, s := range seqs {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: pure string functions, no network ---

// Classify turns any accepted input — a bare UID or a full NCBI nuccore URL —
// into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("unrecognized ncbinucleotide reference: %q", input)
	}
	// Strip URL if given.
	if u, err := url.Parse(input); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		// e.g. https://www.ncbi.nlm.nih.gov/nuccore/3346695951
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		input = parts[len(parts)-1]
	}
	return "sequence", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "sequence" {
		return "", errs.Usage("ncbinucleotide has no resource type %q", uriType)
	}
	return NucleotideURL + "/" + id, nil
}

// mapErr converts a library error into the appropriate kit error kind.
func mapErr(err error) error {
	return err
}
