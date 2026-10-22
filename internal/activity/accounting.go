package activity

// Generation accounting is calibrated against retained heap rather than guessed
// multipliers; TestAccountingCoversRetainedHeap keeps it conservative (never
// below measured heap) without overstating rich windows several times over.
// Repository reads and index builds share these costs so their caps agree.
const (
	// Candidate struct (Item, city key, facet slice headers, revision), its share
	// of slice growth and the slot map entry.
	candidateOverhead = 512
	// Facet slice element plus append growth slack; the string is interned.
	ReferenceBytes = 32
	// Intern/index map entry for a distinct string, excluding its bytes.
	keyOverhead = 64
	// One candidate-slot bitmap per distinct city/category/artist/venue key.
	bitmapBytes = (MaxCandidates + 63) / 64 * 8
)

// stringBytes charges allocator size-class rounding and the string header.
func stringBytes(s string) int {
	if s == "" {
		return 0
	}
	return len(s) + len(s)/8 + 16
}

// CandidateBytes is a candidate's unshared cost: fixed structure, its own
// identifier, title and first name, and facet references. Interned city keys
// and facet values are charged once per generation by the Interner.
func CandidateBytes(c Candidate) int {
	n := candidateOverhead + stringBytes(c.Item.EventID) + stringBytes(c.Item.EventTitle)
	if c.Item.ActorFirstName != nil {
		n += 8 + stringBytes(*c.Item.ActorFirstName)
	}
	return n + ReferenceBytes*(len(c.CategoryIDs)+len(c.ArtistIDs)+len(c.VenueIDs))
}

// Interner shares repeated city and facet strings within a generation and with
// the previous one, so unchanged facets cost one copy, not one per group or
// per generation. Bytes reports the distinct strings this generation uses.
type Interner struct {
	strings map[string]string
	used    map[string]struct{}
	bytes   int
}

func NewInterner(previous Candidates) *Interner {
	in := &Interner{strings: map[string]string{}, used: map[string]struct{}{}}
	for _, c := range previous.Items {
		in.strings[c.Item.City], in.strings[c.CityKey] = c.Item.City, c.CityKey
		for _, ids := range [][]string{c.CategoryIDs, c.ArtistIDs, c.VenueIDs} {
			for _, id := range ids {
				in.strings[id] = id
			}
		}
	}
	return in
}

func (in *Interner) String(s string) string {
	shared, ok := in.strings[s]
	if !ok {
		in.strings[s] = s
		shared = s
	}
	if _, ok := in.used[shared]; !ok {
		in.used[shared] = struct{}{}
		in.bytes += stringBytes(shared) + keyOverhead
	}
	return shared
}

// Intern replaces c's city and facet strings with shared copies. Facet slices
// must belong to c alone; reused slices are charged with Reuse instead.
func (in *Interner) Intern(c *Candidate) {
	c.Item.City = in.String(c.Item.City)
	c.CityKey = in.String(c.CityKey)
	for _, ids := range [][]string{c.CategoryIDs, c.ArtistIDs, c.VenueIDs} {
		for i := range ids {
			ids[i] = in.String(ids[i])
		}
	}
}

// Reuse charges facet slices carried over unchanged from the previous
// generation, whose strings are already shared, without modifying them.
func (in *Interner) Reuse(ids ...[]string) {
	for _, list := range ids {
		for _, id := range list {
			in.String(id)
		}
	}
}

func (in *Interner) Bytes() int { return in.bytes }
