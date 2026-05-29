package cldrpack

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
	"github.com/nmeilick/go-i18n/internal/atomicfile"
	"github.com/nmeilick/go-i18n/locale/cldr"
)

const (
	magicHeader = "GI18NCP1"
	magicChunk  = "CPC1"
	magicProv   = "PVD1"
	magicInt    = "CPI1"

	endianMarker = 0x01020304

	schemaMajor = 1
	schemaMinor = 0
	headerLen   = 128

	sectionEntryLen = 32
	chunkEntryLen   = 48
	chunkHeaderLen  = 48
	providerHdrLen  = providerStringPoolOff + providerStringPoolEntryLen

	codecRaw  = 0
	codecZstd = 1

	hashSHA256 = 1
	sigEd25519 = 1

	nilID = ^uint32(0)

	formatProvider = 1

	sectionFlagRequired   = 1 << 0
	sectionFlagSorted     = 1 << 1
	sectionFlagFixedEntry = 1 << 2
)

var requiredSections = []uint32{
	fourCC("CDIR"),
	fourCC("COVR"),
	fourCC("FEAT"),
	fourCC("KEYS"),
	fourCC("LOCL"),
	fourCC("META"),
	fourCC("ROUT"),
	fourCC("STR0"),
}

var sectionSchemas = map[uint32]uint32{
	fourCC("META"): 16,
	fourCC("STR0"): 0,
	fourCC("FEAT"): 28,
	fourCC("LOCL"): 32,
	fourCC("KEYS"): 16,
	fourCC("COVR"): 12,
	fourCC("ROUT"): 32,
	fourCC("CDIR"): chunkEntryLen,
}

// Codec selects the payload chunk codec for newly built packs.
type Codec uint16

const (
	CodecRaw  Codec = codecRaw
	CodecZstd Codec = codecZstd
)

// Limits are optional policy limits. Zero means unbounded.
type Limits struct {
	MaxPackBytes             int64
	MaxReadAllBytes          int64
	MaxResidentBytes         int64
	MaxChunkPackedBytes      int64
	MaxChunkUnpackedBytes    int64
	MaxDecompressionRatio    int64
	MaxSignatureBytes        int64
	MaxConcurrentChunkLoads  int
	MaxLocaleFallbackDepth   int
	MaxDecodedCacheEntries   int
	MaxDecodedRows           int // Deprecated: use MaxDecodedCacheEntries.
	MaxResidentSectionBytes  int64
	MaxIntegrityTrailerBytes int64
}

// Stats describes pack loading and verification work.
type Stats struct {
	PackBytes         int64 `json:"pack_bytes"`
	ResidentBytes     int64 `json:"resident_bytes"`
	Chunks            int   `json:"chunks"`
	ChunkLoads        int64 `json:"chunk_loads"`
	Decompressions    int64 `json:"decompressions"`
	HashVerified      bool  `json:"hash_verified"`
	SignatureVerified bool  `json:"signature_verified"`
}

// BuildOption configures pack creation.
type BuildOption func(*buildConfig)

type buildConfig struct {
	codec      Codec
	signatures []signatureBuild
}

type signatureBuild struct {
	keyID string
	key   ed25519.PrivateKey
	meta  []byte
}

// WithCodec selects the payload codec. Raw is the default.
func WithCodec(codec Codec) BuildOption {
	return func(c *buildConfig) { c.codec = codec }
}

// WithEd25519Signature adds an optional signature over the pack digest and
// caller-supplied signed metadata.
func WithEd25519Signature(keyID string, private ed25519.PrivateKey, signedMeta []byte) BuildOption {
	return func(c *buildConfig) {
		c.signatures = append(c.signatures, signatureBuild{
			keyID: strings.TrimSpace(keyID),
			key:   append(ed25519.PrivateKey(nil), private...),
			meta:  append([]byte(nil), signedMeta...),
		})
	}
}

// OpenOption configures pack loading.
type OpenOption func(*openConfig)

type openConfig struct {
	verifyHash bool
	verifyKeys map[string]ed25519.PublicKey
	limits     Limits
}

// WithHashVerification controls whole-pack hash verification.
func WithHashVerification(enabled bool) OpenOption {
	return func(c *openConfig) { c.verifyHash = enabled }
}

// WithEd25519Verification requires at least one valid signature from the given
// trusted keys.
func WithEd25519Verification(keys map[string]ed25519.PublicKey) OpenOption {
	return func(c *openConfig) {
		c.verifyKeys = map[string]ed25519.PublicKey{}
		for k, v := range keys {
			c.verifyKeys[k] = append(ed25519.PublicKey(nil), v...)
		}
	}
}

// WithLimits sets optional pack-reader resource limits. Zero fields are
// unbounded.
func WithLimits(limits Limits) OpenOption {
	return func(c *openConfig) { c.limits = limits }
}

// WithReadAllLimit sets the limit used by non-seekable fs.FS loading.
func WithReadAllLimit(limit int64) OpenOption {
	return func(c *openConfig) { c.limits.MaxReadAllBytes = limit }
}

// Build serializes bundle into a deterministic .cldrpack byte slice.
func Build(bundle cldr.Bundle, opts ...BuildOption) ([]byte, error) {
	cfg := buildConfig{codec: CodecRaw}
	for _, opt := range opts {
		opt(&cfg)
	}
	if bundle == nil {
		bundle = cldr.Builtin()
	}
	if bundle.Data() == nil {
		return nil, fmt.Errorf("build cldrpack: bundle has no data provider")
	}
	if cfg.codec != CodecRaw && cfg.codec != CodecZstd {
		return nil, fmt.Errorf("build cldrpack: unsupported codec %d", cfg.codec)
	}
	for _, sig := range cfg.signatures {
		if sig.keyID == "" {
			return nil, fmt.Errorf("build cldrpack: signature key ID is required")
		}
		if l := len(sig.key); l != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("build cldrpack: ed25519 private key has length %d", l)
		}
	}

	data := bundle.Data()
	info := bundle.Info()
	coverage, err := normalizeBuildCoverage(bundle.Coverage())
	if err != nil {
		return nil, err
	}
	providerPayload, records, err := encodeProvider(data)
	if err != nil {
		return nil, err
	}
	chunkPayload := wrapChunk(providerPayload, records)
	packedPayload := chunkPayload
	if cfg.codec == CodecZstd {
		enc, err := zstd.NewWriter(nil)
		if err != nil {
			return nil, fmt.Errorf("create zstd encoder: %w", err)
		}
		packedPayload = enc.EncodeAll(chunkPayload, nil)
		if err := enc.Close(); err != nil {
			return nil, fmt.Errorf("close zstd encoder: %w", err)
		}
	}

	pool := newStringPool()
	meta := encodeMeta(info, pool)
	feat := encodeFeatures(info, coverage, pool)
	locl := encodeLocales(data, pool)
	covr, keys := encodeCoverageAndKeySets(coverage, pool)
	rout := encodeRoutes(coverage)
	str0 := pool.bytes()
	sections := []section{
		{kind: fourCC("META"), elemSize: 16, count: mustU32(len(meta) / 16), data: meta},
		{kind: fourCC("STR0"), data: str0},
		{kind: fourCC("FEAT"), elemSize: 28, count: mustU32(len(feat) / 28), data: feat},
		{kind: fourCC("LOCL"), elemSize: 32, count: mustU32(len(locl) / 32), data: locl},
		{kind: fourCC("KEYS"), elemSize: 16, count: mustU32(len(keys) / 16), data: keys},
		{kind: fourCC("COVR"), elemSize: 12, count: mustU32(len(covr) / 12), data: covr},
		{kind: fourCC("ROUT"), elemSize: 32, count: mustU32(len(rout) / 32), data: rout},
		{kind: fourCC("CDIR"), elemSize: chunkEntryLen, count: 1, data: make([]byte, chunkEntryLen)},
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].kind < sections[j].kind })

	sectionDirOff := uint64(headerLen)
	sectionDirLen := uint64(len(sections)) * sectionEntryLen
	next := alignU64(sectionDirOff + sectionDirLen)
	for i := range sections {
		next = alignU64(next)
		sections[i].offset = next
		sections[i].length = uint64(len(sections[i].data))
		next += sections[i].length
	}
	chunkOff := alignU64(next)
	for i := range sections {
		if sections[i].kind == fourCC("CDIR") {
			sections[i].data = encodeChunkDirectory(chunkOff, uint64(len(packedPayload)), uint64(len(chunkPayload)), records, cfg.codec)
			break
		}
	}
	bodyLen := alignU64(chunkOff + uint64(len(packedPayload)))
	integrityLen := mustU64(integrityLength(cfg.signatures))
	integrityOff := bodyLen

	body := make([]byte, bodyLen)
	writeHeader(body[:headerLen], sectionDirOff, sectionDirLen, mustU32(len(sections)), bodyLen, integrityOff, integrityLen)
	writeSectionDirectory(body[sectionDirOff:sectionDirOff+sectionDirLen], sections)
	for _, sec := range sections {
		copy(body[sec.offset:sec.offset+sec.length], sec.data)
	}
	copy(body[chunkOff:chunkOff+uint64(len(packedPayload))], packedPayload)
	digest := sha256.Sum256(body)
	trailer, err := encodeIntegrity(digest[:], cfg.signatures)
	if err != nil {
		return nil, err
	}
	if uint64(len(trailer)) != integrityLen {
		return nil, fmt.Errorf("build cldrpack: internal integrity length mismatch")
	}
	out := append(body, trailer...)
	return out, nil
}

// WriteFile writes bundle as a .cldrpack file with 0644 permissions.
func WriteFile(path string, bundle cldr.Bundle, opts ...BuildOption) error {
	data, err := Build(bundle, opts...)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, 0o644)
}

// Open opens a file-backed .cldrpack bundle.
func Open(path string, opts ...OpenOption) (cldr.Bundle, error) {
	// #nosec G304 -- opening an application-selected CLDR pack path is this API's purpose.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	b, err := openSource(fileSource{f: f, size: stat.Size(), name: path}, opts...)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return b, nil
}

// FromBytes opens a byte-backed .cldrpack bundle. The input is copied so
// callers may reuse or mutate their original buffer safely.
func FromBytes(name string, data []byte, opts ...OpenOption) (cldr.Bundle, error) {
	cp := append([]byte(nil), data...)
	return openSource(bytesSource{name: name, data: cp}, opts...)
}

// FromEmbedded is an alias for FromBytes that documents embedded-pack usage.
func FromEmbedded(name string, data []byte, opts ...OpenOption) (cldr.Bundle, error) {
	return FromBytes(name, data, opts...)
}

// OpenFS opens a pack from fs.FS. Seekable files are used directly; other files
// are read into memory, subject to an optional read-all limit.
func OpenFS(fsys fs.FS, path string, opts ...OpenOption) (cldr.Bundle, error) {
	cfg := openConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	if ra, ok := f.(interface {
		io.ReaderAt
		io.Closer
		Stat() (fs.FileInfo, error)
	}); ok {
		stat, err := ra.Stat()
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return openSource(readerAtSource{r: ra, close: ra.Close, size: stat.Size(), name: path}, opts...)
	}
	defer f.Close()
	if stat, err := f.Stat(); err == nil && cfg.limits.MaxReadAllBytes > 0 && stat.Size() > cfg.limits.MaxReadAllBytes {
		return nil, fmt.Errorf("open cldrpack %s: read-all size %d exceeds limit %d", path, stat.Size(), cfg.limits.MaxReadAllBytes)
	}
	var r io.Reader = f
	if cfg.limits.MaxReadAllBytes > 0 {
		r = io.LimitReader(f, cfg.limits.MaxReadAllBytes+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if cfg.limits.MaxReadAllBytes > 0 && int64(len(data)) > cfg.limits.MaxReadAllBytes {
		return nil, fmt.Errorf("open cldrpack %s: read-all size exceeds limit %d", path, cfg.limits.MaxReadAllBytes)
	}
	return FromBytes(path, data, opts...)
}

type source interface {
	Size() int64
	ReadAt([]byte, int64) (int, error)
	Slice(int64, int64) ([]byte, bool)
	Close() error
	Name() string
}

type fileSource struct {
	f    *os.File
	size int64
	name string
}

func (s fileSource) Size() int64                             { return s.size }
func (s fileSource) ReadAt(b []byte, off int64) (int, error) { return s.f.ReadAt(b, off) }
func (s fileSource) Slice(int64, int64) ([]byte, bool)       { return nil, false }
func (s fileSource) Close() error                            { return s.f.Close() }
func (s fileSource) Name() string                            { return s.name }

type readerAtSource struct {
	r     io.ReaderAt
	close func() error
	size  int64
	name  string
}

func (s readerAtSource) Size() int64                             { return s.size }
func (s readerAtSource) ReadAt(b []byte, off int64) (int, error) { return s.r.ReadAt(b, off) }
func (s readerAtSource) Slice(int64, int64) ([]byte, bool)       { return nil, false }
func (s readerAtSource) Close() error {
	if s.close != nil {
		return s.close()
	}
	return nil
}
func (s readerAtSource) Name() string { return s.name }

type bytesSource struct {
	data []byte
	name string
}

func (s bytesSource) Size() int64 { return int64(len(s.data)) }
func (s bytesSource) ReadAt(b []byte, off int64) (int, error) {
	if off < 0 || off > int64(len(s.data)) {
		return 0, io.EOF
	}
	start, ok := i64ToInt(off)
	if !ok {
		return 0, io.EOF
	}
	n := copy(b, s.data[start:])
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}
func (s bytesSource) Slice(off, n int64) ([]byte, bool) {
	if off < 0 || n < 0 || off > int64(len(s.data)) || n > int64(len(s.data))-off {
		return nil, false
	}
	start, ok := i64ToInt(off)
	if !ok {
		return nil, false
	}
	length, ok := i64ToInt(n)
	if !ok || start > len(s.data)-length {
		return nil, false
	}
	return s.data[start : start+length], true
}
func (s bytesSource) Close() error { return nil }
func (s bytesSource) Name() string { return s.name }

func openSource(src source, opts ...OpenOption) (cldr.Bundle, error) {
	cfg := openConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.limits.MaxPackBytes > 0 && src.Size() > cfg.limits.MaxPackBytes {
		return nil, fmt.Errorf("open cldrpack %s: pack size %d exceeds limit %d", src.Name(), src.Size(), cfg.limits.MaxPackBytes)
	}
	pr, err := parsePack(src, cfg)
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	info := pr.info
	info.Mode = "external-pack"
	b, err := cldr.NewBundle(info, pr.coverage, pr.provider, src.Close)
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	return b, nil
}

// BundleStats returns pack-loading statistics for bundles opened by this
// package.
func BundleStats(bundle cldr.Bundle) (Stats, bool) {
	if bundle == nil || bundle.Data() == nil {
		return Stats{}, false
	}
	provider, ok := bundle.Data().(*packProvider)
	if !ok || provider.stats == nil {
		return Stats{}, false
	}
	stats := *provider.stats
	stats.ChunkLoads = atomic.LoadInt64(&provider.stats.ChunkLoads)
	stats.Decompressions = atomic.LoadInt64(&provider.stats.Decompressions)
	return stats, true
}

// Validate forces a bundle's advertised resident and payload data through the
// semantic provider API. It is intended for tooling such as bundle checks.
func Validate(bundle cldr.Bundle) error {
	if bundle == nil || bundle.Data() == nil {
		return fmt.Errorf("validate cldrpack: bundle has no data provider")
	}
	data := bundle.Data()
	for _, loc := range data.AvailableLocales() {
		if _, ok := data.Locale(loc); !ok {
			return fmt.Errorf("validate cldrpack: locale %s is advertised but unavailable", loc)
		}
	}
	for _, region := range data.AvailableRegions() {
		if _, ok := data.RegionDefaults(region); !ok {
			return fmt.Errorf("validate cldrpack: region %s is advertised but unavailable", region)
		}
	}
	for _, fraction := range data.AvailableCurrencyFractions() {
		if got := data.CurrencyFraction(fraction.Code); got.Code == "" {
			return fmt.Errorf("validate cldrpack: currency fraction %s is unavailable", fraction.Code)
		}
	}
	for _, symbol := range data.AvailableCurrencySymbols() {
		if symbol.Symbol != "" {
			if _, ok := data.CurrencySymbol(symbol.Locale, symbol.Code, cldr.CurrencyDisplaySymbol); !ok {
				return fmt.Errorf("validate cldrpack: currency symbol %s/%s is advertised but unavailable", symbol.Locale, symbol.Code)
			}
		}
		if symbol.Narrow != "" {
			if _, ok := data.CurrencySymbol(symbol.Locale, symbol.Code, cldr.CurrencyDisplayNarrowSymbol); !ok {
				return fmt.Errorf("validate cldrpack: narrow currency symbol %s/%s is advertised but unavailable", symbol.Locale, symbol.Code)
			}
		}
	}
	for _, rec := range data.AvailableListPatterns() {
		if _, ok := data.ListPattern(rec.Locale, rec.Type, rec.Width); !ok {
			return fmt.Errorf("validate cldrpack: list pattern %s/%s/%s is advertised but unavailable", rec.Locale, rec.Type, rec.Width)
		}
	}
	for _, rec := range data.AvailableUnitPatterns() {
		if _, ok := data.UnitPattern(rec.Locale, rec.Unit, rec.Width, rec.Category); !ok {
			return fmt.Errorf("validate cldrpack: unit pattern %s/%s/%s/%s is advertised but unavailable", rec.Locale, rec.Unit, rec.Width, rec.Category)
		}
	}
	for _, rec := range data.AvailableCompactPatterns() {
		if _, ok := data.CompactPattern(rec.Locale, rec.Width, rec.Magnitude, rec.Category); !ok {
			return fmt.Errorf("validate cldrpack: compact pattern %s/%s/%d/%s is advertised but unavailable", rec.Locale, rec.Width, rec.Magnitude, rec.Category)
		}
	}
	for _, rec := range data.AvailableRelativeTimePatterns() {
		if _, ok := data.RelativeTimePattern(rec.Locale, rec.Field, rec.Width, rec.Direction, rec.Category); !ok {
			return fmt.Errorf("validate cldrpack: relative pattern %s/%s/%s/%s/%s is advertised but unavailable", rec.Locale, rec.Field, rec.Width, rec.Direction, rec.Category)
		}
	}
	for _, rec := range data.AvailableRelativeSpecials() {
		if _, ok := data.RelativeSpecial(rec.Locale, rec.Field, rec.Width, rec.Offset); !ok {
			return fmt.Errorf("validate cldrpack: relative special %s/%s/%s/%d is advertised but unavailable", rec.Locale, rec.Field, rec.Width, rec.Offset)
		}
	}
	for _, rec := range data.AvailableIntervalPatterns() {
		if _, ok := data.IntervalPattern(rec.Locale, rec.Skeleton, rec.Field); !ok {
			return fmt.Errorf("validate cldrpack: interval pattern %s/%s/%s is advertised but unavailable", rec.Locale, rec.Skeleton, rec.Field)
		}
	}
	for _, rec := range data.AvailableDisplayNames() {
		if _, ok := data.DisplayName(rec.Locale, rec.Kind, rec.Code); !ok {
			return fmt.Errorf("validate cldrpack: display name %s/%s/%s is advertised but unavailable", rec.Locale, rec.Kind, rec.Code)
		}
	}
	for _, key := range data.AvailableBCP47Keys() {
		for _, rec := range data.BCP47Types(key) {
			if rec.Key != key {
				return fmt.Errorf("validate cldrpack: BCP47 key %s returned record for %s", key, rec.Key)
			}
		}
	}
	for _, rec := range bundle.Coverage() {
		if rec.All || len(rec.Keys) == 0 {
			continue
		}
		for _, key := range rec.Keys {
			switch rec.Scope {
			case cldr.ScopeLocale:
				if _, ok := data.Locale(key); !ok {
					return fmt.Errorf("validate cldrpack: coverage references unavailable locale %s", key)
				}
			case cldr.ScopeTerritory:
				if _, ok := data.RegionDefaults(key); !ok {
					return fmt.Errorf("validate cldrpack: coverage references unavailable territory %s", key)
				}
			}
		}
	}
	return nil
}

type parseResult struct {
	info     cldr.Info
	coverage []cldr.Coverage
	provider *packProvider
}

type sectionEntry struct {
	kind     uint32
	flags    uint32
	offset   uint64
	length   uint64
	elemSize uint32
	count    uint32
}

type chunkEntry struct {
	chunkID     uint32
	featureID   uint32
	formatID    uint32
	codec       uint16
	flags       uint16
	offset      uint64
	packedLen   uint64
	unpackedLen uint64
	recordCount uint32
}

func parsePack(src source, cfg openConfig) (parseResult, error) {
	if src.Size() < headerLen {
		return parseResult{}, fmt.Errorf("open cldrpack %s: truncated header", src.Name())
	}
	header := make([]byte, headerLen)
	if _, err := src.ReadAt(header, 0); err != nil {
		return parseResult{}, err
	}
	h, err := parseHeader(header, src.Size())
	if err != nil {
		return parseResult{}, err
	}
	if cfg.verifyHash || len(cfg.verifyKeys) > 0 {
		trailer, err := readAtU64(src, h.integrityOff, h.integrityLen)
		if err != nil {
			return parseResult{}, fmt.Errorf("read integrity trailer: %w", err)
		}
		digest, signatures, err := parseIntegrity(trailer, cfg.limits)
		if err != nil {
			return parseResult{}, err
		}
		if cfg.verifyHash || len(cfg.verifyKeys) > 0 {
			bodyLen, ok := u64ToI64(h.bodyLen)
			if !ok {
				return parseResult{}, fmt.Errorf("open cldrpack %s: body too large to hash", src.Name())
			}
			actual, err := hashRange(src, bodyLen)
			if err != nil {
				return parseResult{}, err
			}
			if !bytes.Equal(actual, digest) {
				return parseResult{}, fmt.Errorf("open cldrpack %s: whole-pack hash mismatch", src.Name())
			}
		}
		if len(cfg.verifyKeys) > 0 {
			if err := verifySignatures(digest, signatures, cfg.verifyKeys); err != nil {
				return parseResult{}, err
			}
		}
	}
	entries, err := parseSections(src, h)
	if err != nil {
		return parseResult{}, err
	}
	resident := residentBytes(entries)
	if cfg.limits.MaxResidentBytes > 0 && resident > cfg.limits.MaxResidentBytes {
		return parseResult{}, fmt.Errorf("open cldrpack %s: resident data %d exceeds limit %d", src.Name(), resident, cfg.limits.MaxResidentBytes)
	}
	if err := validateRequiredSections(entries); err != nil {
		return parseResult{}, err
	}
	strPool, err := readSection(src, entries[fourCC("STR0")], cfg.limits)
	if err != nil {
		return parseResult{}, err
	}
	meta, err := parseMeta(src, entries[fourCC("META")], strPool)
	if err != nil {
		return parseResult{}, err
	}
	features, err := parseFeatures(src, entries[fourCC("FEAT")], strPool)
	if err != nil {
		return parseResult{}, err
	}
	locales, err := parseResidentLocales(src, entries[fourCC("LOCL")], strPool)
	if err != nil {
		return parseResult{}, err
	}
	keySets, err := parseKeySets(src, entries[fourCC("KEYS")], strPool)
	if err != nil {
		return parseResult{}, err
	}
	coverage, err := parseCoverage(src, entries[fourCC("COVR")], features, keySets)
	if err != nil {
		return parseResult{}, err
	}
	chunks, err := parseChunkDirectory(src, entries[fourCC("CDIR")])
	if err != nil {
		return parseResult{}, err
	}
	if len(chunks) == 0 {
		return parseResult{}, fmt.Errorf("open cldrpack %s: missing provider chunk", src.Name())
	}
	info := infoFromMeta(meta, features, locales, h)
	provider := &packProvider{
		src:     src,
		chunk:   chunks[0],
		limits:  cfg.limits,
		meta:    metadataFromInfo(info),
		locales: append([]string(nil), locales...),
		stats:   &Stats{PackBytes: src.Size(), ResidentBytes: resident, Chunks: len(chunks), HashVerified: cfg.verifyHash || len(cfg.verifyKeys) > 0, SignatureVerified: len(cfg.verifyKeys) > 0},
	}
	if cfg.limits.MaxConcurrentChunkLoads > 0 {
		provider.loadGate = make(chan struct{}, cfg.limits.MaxConcurrentChunkLoads)
	}
	return parseResult{info: info, coverage: coverage, provider: provider}, nil
}

type parsedHeader struct {
	providerAPIMajor     uint16
	providerAPIMinor     uint16
	featureRegistryMajor uint16
	featureRegistryMinor uint16
	sectionDirOff        uint64
	sectionDirLen        uint64
	sectionCount         uint32
	bodyLen              uint64
	integrityOff         uint64
	integrityLen         uint64
}

func parseHeader(b []byte, size int64) (parsedHeader, error) {
	sizeU64, ok := i64ToU64(size)
	if !ok {
		return parsedHeader{}, fmt.Errorf("open cldrpack: invalid source size")
	}
	if string(b[:8]) != magicHeader {
		return parsedHeader{}, fmt.Errorf("open cldrpack: wrong magic")
	}
	if binary.LittleEndian.Uint32(b[8:12]) != endianMarker {
		return parsedHeader{}, fmt.Errorf("open cldrpack: unsupported endian marker")
	}
	if binary.LittleEndian.Uint16(b[12:14]) != schemaMajor {
		return parsedHeader{}, fmt.Errorf("open cldrpack: unsupported schema major %d", binary.LittleEndian.Uint16(b[12:14]))
	}
	if binary.LittleEndian.Uint32(b[16:20]) != headerLen {
		return parsedHeader{}, fmt.Errorf("open cldrpack: unsupported header length")
	}
	h := parsedHeader{
		providerAPIMajor:     binary.LittleEndian.Uint16(b[24:26]),
		providerAPIMinor:     binary.LittleEndian.Uint16(b[26:28]),
		featureRegistryMajor: binary.LittleEndian.Uint16(b[28:30]),
		featureRegistryMinor: binary.LittleEndian.Uint16(b[30:32]),
		sectionDirOff:        binary.LittleEndian.Uint64(b[32:40]),
		sectionDirLen:        binary.LittleEndian.Uint64(b[40:48]),
		sectionCount:         binary.LittleEndian.Uint32(b[48:52]),
		bodyLen:              binary.LittleEndian.Uint64(b[56:64]),
		integrityOff:         binary.LittleEndian.Uint64(b[64:72]),
		integrityLen:         binary.LittleEndian.Uint64(b[72:80]),
	}
	if h.providerAPIMajor != cldr.ProviderAPIMajor {
		return parsedHeader{}, fmt.Errorf("open cldrpack: unsupported provider API major %d", h.providerAPIMajor)
	}
	if h.featureRegistryMajor != cldr.FeatureRegistryMajor {
		return parsedHeader{}, fmt.Errorf("open cldrpack: unsupported feature registry major %d", h.featureRegistryMajor)
	}
	if binary.LittleEndian.Uint32(b[52:56]) != 0 || !allZero(b[80:128]) {
		return parsedHeader{}, fmt.Errorf("open cldrpack: reserved header bytes are non-zero")
	}
	if h.sectionDirLen != uint64(h.sectionCount)*sectionEntryLen {
		return parsedHeader{}, fmt.Errorf("open cldrpack: invalid section directory length")
	}
	if !rangeOK(h.sectionDirOff, h.sectionDirLen, sizeU64) || !rangeOK(h.integrityOff, h.integrityLen, sizeU64) {
		return parsedHeader{}, fmt.Errorf("open cldrpack: header offsets outside source")
	}
	if h.bodyLen != h.integrityOff || h.bodyLen > sizeU64 {
		return parsedHeader{}, fmt.Errorf("open cldrpack: invalid body length")
	}
	return h, nil
}

func parseSections(src source, h parsedHeader) (map[uint32]sectionEntry, error) {
	data, err := readAtU64(src, h.sectionDirOff, h.sectionDirLen)
	if err != nil {
		return nil, err
	}
	out := map[uint32]sectionEntry{}
	ordered := make([]sectionEntry, 0, h.sectionCount)
	var prev uint32
	for i := uint32(0); i < h.sectionCount; i++ {
		off := i * sectionEntryLen
		rec := sectionEntry{
			kind:     binary.LittleEndian.Uint32(data[off:]),
			flags:    binary.LittleEndian.Uint32(data[off+4:]),
			offset:   binary.LittleEndian.Uint64(data[off+8:]),
			length:   binary.LittleEndian.Uint64(data[off+16:]),
			elemSize: binary.LittleEndian.Uint32(data[off+24:]),
			count:    binary.LittleEndian.Uint32(data[off+28:]),
		}
		if i > 0 && rec.kind <= prev {
			return nil, fmt.Errorf("open cldrpack: section directory is not sorted")
		}
		prev = rec.kind
		sizeU64, ok := i64ToU64(src.Size())
		if !ok || !rangeOK(rec.offset, rec.length, sizeU64) {
			return nil, fmt.Errorf("open cldrpack: section %s outside source", fourCCString(rec.kind))
		}
		if rec.offset < h.sectionDirOff+h.sectionDirLen {
			return nil, fmt.Errorf("open cldrpack: section %s overlaps header or directory", fourCCString(rec.kind))
		}
		if expected, known := sectionSchemas[rec.kind]; known {
			if rec.elemSize != expected {
				return nil, fmt.Errorf("open cldrpack: section %s has elem size %d, want %d", fourCCString(rec.kind), rec.elemSize, expected)
			}
		} else if rec.flags&sectionFlagRequired != 0 {
			return nil, fmt.Errorf("open cldrpack: unknown required section %s", fourCCString(rec.kind))
		}
		wantLen, ok := checkedMulU64(uint64(rec.elemSize), uint64(rec.count))
		if rec.elemSize > 0 && (!ok || rec.length != wantLen) {
			return nil, fmt.Errorf("open cldrpack: section %s length/count mismatch", fourCCString(rec.kind))
		}
		ordered = append(ordered, rec)
		out[rec.kind] = rec
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].offset < ordered[j].offset })
	var prevEnd uint64
	for i, rec := range ordered {
		if i > 0 && rec.offset < prevEnd {
			return nil, fmt.Errorf("open cldrpack: section %s overlaps another section", fourCCString(rec.kind))
		}
		prevEnd = rec.offset + rec.length
	}
	return out, nil
}

func validateRequiredSections(entries map[uint32]sectionEntry) error {
	for _, kind := range requiredSections {
		if _, ok := entries[kind]; !ok {
			return fmt.Errorf("open cldrpack: missing required section %s", fourCCString(kind))
		}
	}
	return nil
}

func readSection(src source, sec sectionEntry, limits Limits) ([]byte, error) {
	length, ok := u64ToI64(sec.length)
	if !ok {
		return nil, fmt.Errorf("open cldrpack: resident section %s too large", fourCCString(sec.kind))
	}
	if limits.MaxResidentSectionBytes > 0 && length > limits.MaxResidentSectionBytes {
		return nil, fmt.Errorf("open cldrpack: resident section %s exceeds limit", fourCCString(sec.kind))
	}
	return readAtU64(src, sec.offset, sec.length)
}

func parseMeta(src source, sec sectionEntry, pool []byte) (map[string]string, error) {
	data, err := readAtU64(src, sec.offset, sec.length)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for off := 0; off < len(data); off += 16 {
		key, err := readStringRef(pool, data[off:off+8])
		if err != nil {
			return nil, err
		}
		value, err := readStringRef(pool, data[off+8:off+16])
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

func parseFeatures(src source, sec sectionEntry, pool []byte) (map[uint32]cldr.FeatureID, error) {
	data, err := readAtU64(src, sec.offset, sec.length)
	if err != nil {
		return nil, err
	}
	out := map[uint32]cldr.FeatureID{}
	for off := 0; off < len(data); off += 28 {
		id := binary.LittleEndian.Uint32(data[off:])
		name, err := readStringRef(pool, data[off+4:off+12])
		if err != nil {
			return nil, err
		}
		if _, ok := statusFromWire(data[off+20]); !ok {
			return nil, fmt.Errorf("open cldrpack: feature %d has unsupported status %d", id, data[off+20])
		}
		if !allZero(data[off+21 : off+24]) {
			return nil, fmt.Errorf("open cldrpack: feature reserved bytes are non-zero")
		}
		out[id] = cldr.FeatureID(name)
	}
	return out, nil
}

func parseResidentLocales(src source, sec sectionEntry, pool []byte) ([]string, error) {
	data, err := readAtU64(src, sec.offset, sec.length)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for off := 0; off < len(data); off += 32 {
		tag, err := readStringRef(pool, data[off+4:off+12])
		if err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}

type coverageKeySet struct {
	scope cldr.ScopeKind
	keys  []string
}

func parseKeySets(src source, sec sectionEntry, pool []byte) (map[uint32]coverageKeySet, error) {
	data, err := readAtU64(src, sec.offset, sec.length)
	if err != nil {
		return nil, err
	}
	out := map[uint32]coverageKeySet{}
	for off := 0; off < len(data); off += 16 {
		setID := binary.LittleEndian.Uint32(data[off:])
		if setID == nilID || setID == 0 {
			return nil, fmt.Errorf("open cldrpack: invalid key set id %d", setID)
		}
		scope, ok := scopeFromWire(binary.LittleEndian.Uint16(data[off+4:]))
		if !ok {
			return nil, fmt.Errorf("open cldrpack: key set %d has unsupported scope %d", setID, binary.LittleEndian.Uint16(data[off+4:]))
		}
		if binary.LittleEndian.Uint16(data[off+6:]) != 0 {
			return nil, fmt.Errorf("open cldrpack: key set reserved bytes are non-zero")
		}
		key, err := readStringRef(pool, data[off+8:off+16])
		if err != nil {
			return nil, err
		}
		set := out[setID]
		if set.scope != "" && set.scope != scope {
			return nil, fmt.Errorf("open cldrpack: key set %d mixes scopes", setID)
		}
		set.scope = scope
		set.keys = append(set.keys, key)
		out[setID] = set
	}
	for id, set := range out {
		sort.Strings(set.keys)
		out[id] = set
	}
	return out, nil
}

func parseCoverage(src source, sec sectionEntry, features map[uint32]cldr.FeatureID, keySets map[uint32]coverageKeySet) ([]cldr.Coverage, error) {
	data, err := readAtU64(src, sec.offset, sec.length)
	if err != nil {
		return nil, err
	}
	out := []cldr.Coverage{}
	for off := 0; off < len(data); off += 12 {
		featureID := binary.LittleEndian.Uint32(data[off:])
		feature, ok := features[featureID]
		if !ok {
			return nil, fmt.Errorf("open cldrpack: coverage references unknown feature %d", featureID)
		}
		scopeRaw := binary.LittleEndian.Uint16(data[off+4:])
		scope, ok := scopeFromWire(scopeRaw)
		if !ok {
			return nil, fmt.Errorf("open cldrpack: coverage for feature %d has unsupported scope %d", featureID, scopeRaw)
		}
		roleRaw := data[off+6]
		role, ok := roleFromWire(roleRaw)
		if !ok {
			return nil, fmt.Errorf("open cldrpack: coverage for feature %d has unsupported role %d", featureID, roleRaw)
		}
		statusRaw := data[off+7]
		status, ok := statusFromWire(statusRaw)
		if !ok {
			return nil, fmt.Errorf("open cldrpack: coverage for feature %d has unsupported status %d", featureID, statusRaw)
		}
		scopeSet := binary.LittleEndian.Uint32(data[off+8:])
		rec := cldr.Coverage{Feature: feature, Scope: scope, Role: role, Status: status}
		if scopeSet == nilID {
			rec.All = true
		} else {
			set, ok := keySets[scopeSet]
			if !ok {
				return nil, fmt.Errorf("open cldrpack: coverage references unknown key set %d", scopeSet)
			}
			if set.scope != rec.Scope {
				return nil, fmt.Errorf("open cldrpack: coverage key set %d scope mismatch", scopeSet)
			}
			rec.Keys = append([]string(nil), set.keys...)
		}
		out = append(out, rec)
	}
	return out, nil
}

func parseChunkDirectory(src source, sec sectionEntry) ([]chunkEntry, error) {
	data, err := readAtU64(src, sec.offset, sec.length)
	if err != nil {
		return nil, err
	}
	out := []chunkEntry{}
	for off := 0; off < len(data); off += chunkEntryLen {
		rec := chunkEntry{
			chunkID:     binary.LittleEndian.Uint32(data[off:]),
			featureID:   binary.LittleEndian.Uint32(data[off+4:]),
			formatID:    binary.LittleEndian.Uint32(data[off+8:]),
			codec:       binary.LittleEndian.Uint16(data[off+12:]),
			flags:       binary.LittleEndian.Uint16(data[off+14:]),
			offset:      binary.LittleEndian.Uint64(data[off+16:]),
			packedLen:   binary.LittleEndian.Uint64(data[off+24:]),
			unpackedLen: binary.LittleEndian.Uint64(data[off+32:]),
			recordCount: binary.LittleEndian.Uint32(data[off+40:]),
		}
		if binary.LittleEndian.Uint32(data[off+44:]) != 0 {
			return nil, fmt.Errorf("open cldrpack: chunk reserved bytes are non-zero")
		}
		if rec.codec != codecRaw && rec.codec != codecZstd {
			return nil, fmt.Errorf("open cldrpack: unsupported codec %d", rec.codec)
		}
		sizeU64, ok := i64ToU64(src.Size())
		if !ok || !rangeOK(rec.offset, rec.packedLen, sizeU64) {
			return nil, fmt.Errorf("open cldrpack: chunk outside source")
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].chunkID < out[j].chunkID })
	return out, nil
}

func infoFromMeta(meta map[string]string, features map[uint32]cldr.FeatureID, locales []string, h parsedHeader) cldr.Info {
	featureList := make([]cldr.FeatureID, 0, len(features))
	for _, feature := range features {
		featureList = append(featureList, feature)
	}
	sort.Slice(featureList, func(i, j int) bool { return featureList[i] < featureList[j] })
	return cldr.Info{
		ID:   meta["id"],
		Name: meta["name"],
		Mode: meta["mode"],
		Versions: cldr.Versions{
			CLDR:                 meta["cldr"],
			Unicode:              meta["unicode"],
			TZDB:                 meta["tzdb"],
			SchemaMajor:          schemaMajor,
			SchemaMinor:          schemaMinor,
			ProviderAPIMajor:     h.providerAPIMajor,
			ProviderAPIMinor:     h.providerAPIMinor,
			FeatureRegistryMajor: h.featureRegistryMajor,
			FeatureRegistryMinor: h.featureRegistryMinor,
			Generator:            meta["generator"],
			SourceIdentity:       meta["source_identity"],
			SourceDigest:         meta["source_digest"],
			License:              meta["license"],
		},
		Features: featureList,
		Locales:  locales,
		Digest:   meta["digest"],
	}
}

func metadataFromInfo(info cldr.Info) cldr.Metadata {
	return cldr.Metadata{
		CLDRVersion:    info.Versions.CLDR,
		UnicodeVersion: info.Versions.Unicode,
		TZDBVersion:    info.Versions.TZDB,
		Generator:      info.Versions.Generator,
		SourceIdentity: info.Versions.SourceIdentity,
		TreeDigest:     info.Versions.SourceDigest,
		License:        info.Versions.License,
	}
}

type packProvider struct {
	src      source
	chunk    chunkEntry
	limits   Limits
	meta     cldr.Metadata
	locales  []string
	stats    *Stats
	loadGate chan struct{}

	once sync.Once
	data providerData
	err  error

	cacheMu       sync.RWMutex
	localeCache   map[string]cldr.LocaleRecord
	regionCache   map[string]cldr.RegionDefaults
	fractionCache map[string]cldr.CurrencyFraction
	symbolCache   map[string]string
	bcp47Cache    map[string][]cldr.BCP47TypeRecord
	cacheEntries  int
}

func (p *packProvider) Metadata() cldr.Metadata { return p.meta }

func (p *packProvider) Locale(tag string) (cldr.LocaleRecord, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		tag = "en"
	}
	p.cacheMu.RLock()
	if p.localeCache != nil {
		if rec, ok := p.localeCache[tag]; ok {
			p.cacheMu.RUnlock()
			return rec, true
		}
	}
	p.cacheMu.RUnlock()
	if err := p.load(); err != nil {
		return cldr.LocaleRecord{}, false
	}
	rec, ok := p.data.localeExact(tag)
	if !ok {
		canonical := cldr.CanonicalTag(tag)
		rec, ok = p.data.localeExact(canonical)
		tag = canonical
	}
	if !ok {
		return cldr.LocaleRecord{}, false
	}
	p.cacheMu.Lock()
	if p.canCacheLocked() {
		if p.localeCache == nil {
			p.localeCache = map[string]cldr.LocaleRecord{}
		}
		p.localeCache[tag] = rec
		p.cacheEntries++
	}
	p.cacheMu.Unlock()
	return rec, true
}

func (p *packProvider) Parent(tag string) (string, bool) {
	rec, ok := p.Locale(tag)
	if !ok || rec.Parent == "" {
		return "", false
	}
	return rec.Parent, true
}

func (p *packProvider) RegionDefaults(region string) (cldr.RegionDefaults, bool) {
	region = strings.ToUpper(strings.TrimSpace(region))
	p.cacheMu.RLock()
	if p.regionCache != nil {
		if rec, ok := p.regionCache[region]; ok {
			p.cacheMu.RUnlock()
			return rec, true
		}
	}
	p.cacheMu.RUnlock()
	if err := p.load(); err != nil {
		return cldr.RegionDefaults{}, false
	}
	rec, ok := p.data.region(region)
	if !ok {
		return cldr.RegionDefaults{}, false
	}
	p.cacheMu.Lock()
	if p.canCacheLocked() {
		if p.regionCache == nil {
			p.regionCache = map[string]cldr.RegionDefaults{}
		}
		p.regionCache[region] = rec
		p.cacheEntries++
	}
	p.cacheMu.Unlock()
	return rec, true
}

func (p *packProvider) CurrencyFraction(code string) cldr.CurrencyFraction {
	code = strings.ToUpper(strings.TrimSpace(code))
	p.cacheMu.RLock()
	if p.fractionCache != nil {
		if rec, ok := p.fractionCache[code]; ok {
			p.cacheMu.RUnlock()
			return rec
		}
	}
	p.cacheMu.RUnlock()
	if err := p.load(); err != nil {
		return cldr.CurrencyFraction{Code: code, Digits: 2, CashDigits: 2}
	}
	rec := p.data.fraction(code)
	p.cacheMu.Lock()
	if p.canCacheLocked() {
		if p.fractionCache == nil {
			p.fractionCache = map[string]cldr.CurrencyFraction{}
		}
		p.fractionCache[code] = rec
		p.cacheEntries++
	}
	p.cacheMu.Unlock()
	return rec
}

func (p *packProvider) CurrencySymbol(locale, code string, display cldr.CurrencyDisplayMode) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if display == cldr.CurrencyDisplayCode {
		return code, code != ""
	}
	locale = cldr.CanonicalTag(locale)
	key := locale + "\x00" + code + "\x00" + string(display)
	p.cacheMu.RLock()
	if p.symbolCache != nil {
		if sym, ok := p.symbolCache[key]; ok {
			p.cacheMu.RUnlock()
			return sym, true
		}
	}
	p.cacheMu.RUnlock()
	if err := p.load(); err != nil {
		return "", false
	}
	sym, ok := p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.symbol(cur, code, display)
	})
	if !ok {
		return "", false
	}
	p.cacheMu.Lock()
	if p.canCacheLocked() {
		if p.symbolCache == nil {
			p.symbolCache = map[string]string{}
		}
		p.symbolCache[key] = sym
		p.cacheEntries++
	}
	p.cacheMu.Unlock()
	return sym, true
}

func (p *packProvider) ListPattern(locale, typ, width string) (cldr.ListPattern, bool) {
	if err := p.load(); err != nil {
		return cldr.ListPattern{}, false
	}
	return p.lookupListPatternByLocale(locale, func(cur string) (cldr.ListPattern, bool) {
		return p.data.listPattern(cur, typ, width)
	})
}

func (p *packProvider) UnitPattern(locale, unit, width, category string) (string, bool) {
	if err := p.load(); err != nil {
		return "", false
	}
	return p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.unitPattern(cur, unit, width, category)
	})
}

func (p *packProvider) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	if err := p.load(); err != nil {
		return "", false
	}
	return p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.compactPattern(cur, width, magnitude, category)
	})
}

func (p *packProvider) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	if err := p.load(); err != nil {
		return "", false
	}
	return p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.relativePattern(cur, field, width, direction, category)
	})
}

func (p *packProvider) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	if err := p.load(); err != nil {
		return "", false
	}
	return p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.relativeSpecial(cur, field, width, offset)
	})
}

func (p *packProvider) IntervalPattern(locale, skeleton, field string) (string, bool) {
	if err := p.load(); err != nil {
		return "", false
	}
	return p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.intervalPattern(cur, skeleton, field)
	})
}

func (p *packProvider) DisplayName(locale, kind, code string) (string, bool) {
	if err := p.load(); err != nil {
		return "", false
	}
	return p.lookupStringByLocale(locale, func(cur string) (string, bool) {
		return p.data.displayName(cur, kind, code)
	})
}

func (p *packProvider) BCP47Types(key string) []cldr.BCP47TypeRecord {
	key = strings.TrimSpace(key)
	p.cacheMu.RLock()
	if p.bcp47Cache != nil {
		if recs, ok := p.bcp47Cache[key]; ok {
			p.cacheMu.RUnlock()
			return append([]cldr.BCP47TypeRecord(nil), recs...)
		}
	}
	p.cacheMu.RUnlock()
	if err := p.load(); err != nil {
		return nil
	}
	recs := p.data.bcp47(key)
	p.cacheMu.Lock()
	if p.canCacheLocked() {
		if p.bcp47Cache == nil {
			p.bcp47Cache = map[string][]cldr.BCP47TypeRecord{}
		}
		p.bcp47Cache[key] = append([]cldr.BCP47TypeRecord(nil), recs...)
		p.cacheEntries++
	}
	p.cacheMu.Unlock()
	return recs
}

func (p *packProvider) lookupStringByLocale(locale string, lookup func(string) (string, bool)) (string, bool) {
	for cur := cldr.CanonicalTag(locale); cur != ""; {
		if value, ok := lookup(cur); ok && value != "" {
			return value, true
		}
		parent, ok := p.Parent(cur)
		if !ok || parent == cur {
			parent = packParentTag(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	return lookup("en")
}

func (p *packProvider) lookupListPatternByLocale(locale string, lookup func(string) (cldr.ListPattern, bool)) (cldr.ListPattern, bool) {
	for cur := cldr.CanonicalTag(locale); cur != ""; {
		if value, ok := lookup(cur); ok {
			return value, true
		}
		parent, ok := p.Parent(cur)
		if !ok || parent == cur {
			parent = packParentTag(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	return lookup("en")
}

func packParentTag(tag string) string {
	if i := strings.LastIndex(tag, "-"); i > 0 {
		return tag[:i]
	}
	return ""
}

func (p *packProvider) canCacheLocked() bool {
	limit := p.limits.MaxDecodedCacheEntries
	if limit <= 0 {
		limit = p.limits.MaxDecodedRows
	}
	return limit <= 0 || p.cacheEntries < limit
}

func (p *packProvider) AvailableLocales() []string {
	return append([]string(nil), p.locales...)
}

func (p *packProvider) AvailableRegions() []string {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.regions()
}

func (p *packProvider) AvailableCurrencyFractions() []cldr.CurrencyFraction {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.fractions()
}

func (p *packProvider) AvailableCurrencySymbols() []cldr.CurrencySymbolRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.symbols()
}

func (p *packProvider) AvailableListPatterns() []cldr.ListPatternRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.listPatterns()
}

func (p *packProvider) AvailableUnitPatterns() []cldr.UnitPatternRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.unitPatterns()
}

func (p *packProvider) AvailableCompactPatterns() []cldr.CompactPatternRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.compactPatterns()
}

func (p *packProvider) AvailableRelativeTimePatterns() []cldr.RelativePatternRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.relativePatterns()
}

func (p *packProvider) AvailableRelativeSpecials() []cldr.RelativeSpecialRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.relativeSpecials()
}

func (p *packProvider) AvailableIntervalPatterns() []cldr.IntervalPatternRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.intervalPatterns()
}

func (p *packProvider) AvailableDisplayNames() []cldr.DisplayNameRecord {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.displayNames()
}

func (p *packProvider) AvailableBCP47Keys() []string {
	if err := p.load(); err != nil {
		return nil
	}
	return p.data.bcp47Keys()
}

func (p *packProvider) load() error {
	p.once.Do(func() {
		p.err = p.loadOnce()
	})
	return p.err
}

func (p *packProvider) loadOnce() error {
	if p.loadGate != nil {
		p.loadGate <- struct{}{}
		defer func() { <-p.loadGate }()
	}
	packedLen, ok := u64ToI64(p.chunk.packedLen)
	if !ok {
		return fmt.Errorf("open cldrpack: packed chunk too large")
	}
	unpackedLen, ok := u64ToI64(p.chunk.unpackedLen)
	if !ok {
		return fmt.Errorf("open cldrpack: unpacked chunk too large")
	}
	if p.limits.MaxChunkPackedBytes > 0 && packedLen > p.limits.MaxChunkPackedBytes {
		return fmt.Errorf("open cldrpack: packed chunk exceeds limit")
	}
	if p.limits.MaxChunkUnpackedBytes > 0 && unpackedLen > p.limits.MaxChunkUnpackedBytes {
		return fmt.Errorf("open cldrpack: unpacked chunk exceeds limit")
	}
	packed, err := readAtU64(p.src, p.chunk.offset, p.chunk.packedLen)
	if err != nil {
		return err
	}
	atomicAdd(&p.stats.ChunkLoads, 1)
	payload := packed
	if p.chunk.codec == codecZstd {
		if p.limits.MaxDecompressionRatio > 0 && packedLen > 0 {
			limit, overflow := checkedMulI64(p.limits.MaxDecompressionRatio, packedLen)
			if overflow || unpackedLen > limit {
				return fmt.Errorf("open cldrpack: decompression ratio exceeds limit")
			}
		}
		options := []zstd.DOption{zstd.WithDecodeAllCapLimit(true)}
		if p.chunk.unpackedLen > 0 {
			options = append(options, zstd.WithDecoderMaxMemory(p.chunk.unpackedLen))
		}
		dec, err := zstd.NewReader(nil, options...)
		if err != nil {
			return fmt.Errorf("create zstd decoder: %w", err)
		}
		defer dec.Close()
		dstLen, ok := i64ToInt(unpackedLen)
		if !ok {
			return fmt.Errorf("open cldrpack: unpacked chunk too large")
		}
		payload, err = dec.DecodeAll(packed, make([]byte, 0, dstLen))
		if err != nil {
			return fmt.Errorf("decode zstd chunk: %w", err)
		}
		atomicAdd(&p.stats.Decompressions, 1)
	}
	if uint64(len(payload)) != p.chunk.unpackedLen {
		return fmt.Errorf("open cldrpack: unpacked chunk length mismatch")
	}
	providerPayload, err := unwrapChunk(payload)
	if err != nil {
		return err
	}
	data, err := parseProvider(providerPayload)
	if err != nil {
		return err
	}
	p.data = data
	return nil
}

func atomicAdd(ptr *int64, n int64) {
	if ptr != nil {
		atomic.AddInt64(ptr, n)
	}
}

func checkedMulI64(a, b int64) (int64, bool) {
	if a < 0 || b < 0 {
		return 0, true
	}
	if a == 0 || b == 0 {
		return 0, false
	}
	if a > int64(^uint64(0)>>1)/b {
		return 0, true
	}
	return a * b, false
}
