package cldrpack

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/locale/cldr"
)

func TestBuildOpenParityWithBuiltin(t *testing.T) {
	pack, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := FromBytes("core.cldrpack", pack, WithHashVerification(true))
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if bundle.Info().Versions.CLDR != cldr.Builtin().Info().Versions.CLDR {
		t.Fatalf("CLDR version = %q", bundle.Info().Versions.CLDR)
	}
	got, ok := bundle.Data().Locale("ar")
	if !ok || got.NumberingSystem == "" || got.MonthsWide[0] == "" {
		t.Fatalf("locale ar = %#v ok=%v", got, ok)
	}
	want, _ := cldr.Builtin().Data().Locale("ar")
	if got.NumberingSystem != want.NumberingSystem || got.DateFormats != want.DateFormats {
		t.Fatalf("pack locale differs from built-in")
	}
	profile, err := locale.NewProfile([]string{"fr-FR"}, locale.WithCurrency(locale.Currency("EUR")))
	if err != nil {
		t.Fatal(err)
	}
	ctx := locale.FormatContext{Profile: profile, Locale: profile.PrimaryLanguage()}
	spec := locale.CurrencySpec{Value: 42, Code: locale.Currency("EUR"), CodeSource: locale.CurrencyExplicit}
	packSvc, err := cldr.NewServices(bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer packSvc.Close()
	gotText, gotDiag := packSvc.Formatter().FormatCurrency(ctx, spec)
	wantText, wantDiag := locale.DefaultFormatter().FormatCurrency(ctx, spec)
	if gotText != wantText || len(gotDiag) != len(wantDiag) {
		t.Fatalf("pack formatter = %q %#v, default = %q %#v", gotText, gotDiag, wantText, wantDiag)
	}
}

func TestHashMismatchIsRejectedWhenVerificationRequested(t *testing.T) {
	pack, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	pack[headerLen+4] ^= 0xff
	if _, err := FromBytes("corrupt.cldrpack", pack, WithHashVerification(true)); err == nil ||
		!strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
}

func TestZstdChunkIsLazyAndVerified(t *testing.T) {
	pack, err := Build(cldr.Builtin(), WithCodec(CodecZstd))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := FromBytes("core-zstd.cldrpack", pack, WithHashVerification(true))
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := BundleStats(bundle)
	if !ok {
		t.Fatal("expected pack stats")
	}
	if stats.Decompressions != 0 || stats.ChunkLoads != 0 {
		t.Fatalf("chunk loaded during open: %#v", stats)
	}
	if _, ok := bundle.Data().Locale("de-CH"); !ok {
		t.Fatal("missing de-CH")
	}
	stats, _ = BundleStats(bundle)
	if stats.Decompressions != 1 || stats.ChunkLoads != 1 {
		t.Fatalf("zstd stats = %#v", stats)
	}
}

func TestValidateForcesLazyPayloadLoad(t *testing.T) {
	pack, err := Build(cldr.Builtin(), WithCodec(CodecZstd))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := FromBytes("core-zstd.cldrpack", pack)
	if err != nil {
		t.Fatal(err)
	}
	if stats, _ := BundleStats(bundle); stats.ChunkLoads != 0 {
		t.Fatalf("chunk loaded during open: %#v", stats)
	}
	if err := Validate(bundle); err != nil {
		t.Fatal(err)
	}
	if stats, _ := BundleStats(bundle); stats.ChunkLoads != 1 || stats.Decompressions != 1 {
		t.Fatalf("validate did not force payload load: %#v", stats)
	}
}

func TestSelectedLocaleCoverageSurvivesPackRoundTrip(t *testing.T) {
	selected, plan, err := cldr.SelectBundle(cldr.Builtin(), cldr.Selection{
		Locales:  []string{"de"},
		Features: []cldr.FeatureID{cldr.FeatureNumbersDecimal},
	})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := Build(selected)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := FromBytes("selected.cldrpack", pack)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if err := Validate(bundle); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, loc := range plan.Locales {
		want[loc] = true
	}
	found := false
	for _, rec := range bundle.Coverage() {
		if rec.Feature != cldr.FeatureNumbersDecimal || rec.Scope != cldr.ScopeLocale {
			continue
		}
		found = true
		if rec.All {
			t.Fatalf("selected locale coverage widened to all: %#v", rec)
		}
		for _, key := range rec.Keys {
			if !want[key] {
				t.Fatalf("coverage key %q not in selected locales %#v", key, plan.Locales)
			}
		}
	}
	if !found {
		t.Fatal("missing selected locale coverage")
	}
}

func TestSignatureVerification(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := Build(cldr.Builtin(), WithEd25519Signature("test-key", priv, []byte("fixture")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromBytes("signed.cldrpack", pack); err != nil {
		t.Fatalf("signature should be ignored unless requested: %v", err)
	}
	verified, err := FromBytes("signed.cldrpack", pack, WithEd25519Verification(map[string]ed25519.PublicKey{"test-key": pub}))
	if err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	stats, ok := BundleStats(verified)
	if !ok || !stats.HashVerified || !stats.SignatureVerified {
		t.Fatalf("verification stats = %#v ok=%v", stats, ok)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromBytes("signed.cldrpack", pack, WithEd25519Verification(map[string]ed25519.PublicKey{"test-key": otherPub})); err == nil {
		t.Fatal("expected invalid signature error")
	}
	unsigned, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromBytes("unsigned.cldrpack", unsigned, WithEd25519Verification(map[string]ed25519.PublicKey{"test-key": pub})); err == nil {
		t.Fatal("expected missing signature verification error")
	}
}

func TestOpenFSReadAllLimitAndUnboundedDefault(t *testing.T) {
	pack, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	fsys := nonSeekFS{data: map[string][]byte{"core.cldrpack": pack}}
	if _, err := OpenFS(fsys, "core.cldrpack", WithReadAllLimit(16)); err == nil {
		t.Fatal("expected read-all limit error")
	}
	bundle, err := OpenFS(fsys, "core.cldrpack")
	if err != nil {
		t.Fatalf("unbounded default rejected pack: %v", err)
	}
	defer bundle.Close()
	if _, ok := bundle.Data().Locale("en"); !ok {
		t.Fatal("missing en")
	}
}

func TestPackLimitsAndProviderRefValidation(t *testing.T) {
	pack, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromBytes("limited.cldrpack", pack, WithLimits(Limits{MaxResidentBytes: 1})); err == nil ||
		!strings.Contains(err.Error(), "resident data") {
		t.Fatalf("expected resident limit error, got %v", err)
	}

	bad := append([]byte(nil), pack...)
	corruptFirstProviderStringRef(t, bad)
	bundle, err := FromBytes("bad-provider.cldrpack", bad)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bundle.Data().Locale("en"); ok {
		t.Fatal("corrupt provider string ref was accepted")
	}
}

func TestMalformedPackErrors(t *testing.T) {
	pack, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	t.Run("bad magic", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		copy(bad[:8], "BADPACK!")
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "magic") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unsupported codec", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchFirstChunkCodec(t, bad, 99)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unsupported codec") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("chunk outside source", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchFirstChunkOffset(t, bad, uint64(len(bad)+1024))
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "chunk outside source") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unsupported provider major", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		binary.LittleEndian.PutUint16(bad[24:26], cldr.ProviderAPIMajor+1)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "provider API major") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("fixed section schema mismatch", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchSectionElemSize(t, bad, fourCC("COVR"), 99)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "elem size") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unknown required section", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchSectionKindAndFlags(t, bad, fourCC("COVR"), fourCC("COVS"), sectionFlagRequired)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unknown required section") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid feature status", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchFirstFeatureStatus(t, bad, 99)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unsupported status") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid coverage scope", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchFirstCoverageScope(t, bad, 65000)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unsupported scope") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid coverage role", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchFirstCoverageRole(t, bad, 99)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unsupported role") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid coverage status", func(t *testing.T) {
		bad := append([]byte(nil), pack...)
		patchFirstCoverageStatus(t, bad, 99)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unsupported status") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid key set scope", func(t *testing.T) {
		bad := selectedLocalePack(t)
		patchFirstKeySetScope(t, bad, 65000)
		if _, err := FromBytes("bad.cldrpack", bad); err == nil || !strings.Contains(err.Error(), "unsupported scope") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestParseSectionsSkipsUnknownOptionalSection(t *testing.T) {
	data := make([]byte, headerLen+sectionEntryLen)
	binary.LittleEndian.PutUint32(data[headerLen:], fourCC("XOPT"))
	binary.LittleEndian.PutUint64(data[headerLen+8:], uint64(len(data)))
	h := parsedHeader{sectionDirOff: headerLen, sectionDirLen: sectionEntryLen, sectionCount: 1}
	entries, err := parseSections(bytesSource{name: "optional.cldrpack", data: data}, h)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries[fourCC("XOPT")]; !ok {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestOpenFileClosesOnParseError(t *testing.T) {
	fsys := nonSeekFS{data: map[string][]byte{"bad.cldrpack": []byte("bad")}}
	if _, err := OpenFS(fsys, "bad.cldrpack"); err == nil {
		t.Fatal("expected parse error")
	}
}

type nonSeekFS struct {
	fs.FS
	data map[string][]byte
}

func (f nonSeekFS) Open(name string) (fs.File, error) {
	if f.FS != nil {
		return f.FS.Open(name)
	}
	data, ok := f.data[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &nonSeekFile{name: name, r: bytes.NewReader(data), data: data}, nil
}

type nonSeekFile struct {
	name string
	r    *bytes.Reader
	data []byte
}

func (f *nonSeekFile) Stat() (fs.FileInfo, error) {
	return nonSeekInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *nonSeekFile) Read(b []byte) (int, error) { return f.r.Read(b) }
func (f *nonSeekFile) Close() error               { return nil }

type nonSeekInfo struct {
	name string
	size int64
}

func (i nonSeekInfo) Name() string       { return i.name }
func (i nonSeekInfo) Size() int64        { return i.size }
func (i nonSeekInfo) Mode() fs.FileMode  { return 0o644 }
func (i nonSeekInfo) ModTime() time.Time { return time.Time{} }
func (i nonSeekInfo) IsDir() bool        { return false }
func (i nonSeekInfo) Sys() any           { return nil }

func patchFirstChunkCodec(t *testing.T, pack []byte, codec uint16) {
	t.Helper()
	cdir := sectionData(t, pack, fourCC("CDIR"))
	binary.LittleEndian.PutUint16(cdir[12:14], codec)
}

func patchFirstChunkOffset(t *testing.T, pack []byte, off uint64) {
	t.Helper()
	cdir := sectionData(t, pack, fourCC("CDIR"))
	binary.LittleEndian.PutUint64(cdir[16:24], off)
}

func patchSectionElemSize(t *testing.T, pack []byte, kind uint32, elemSize uint32) {
	t.Helper()
	dirOff := binary.LittleEndian.Uint64(pack[32:40])
	count := int(binary.LittleEndian.Uint32(pack[48:52]))
	for i := 0; i < count; i++ {
		rec := pack[int(dirOff)+i*sectionEntryLen:]
		if binary.LittleEndian.Uint32(rec[:4]) == kind {
			binary.LittleEndian.PutUint32(rec[24:28], elemSize)
			return
		}
	}
	t.Fatalf("section %s not found", fourCCString(kind))
}

func patchSectionKindAndFlags(t *testing.T, pack []byte, from, to, flags uint32) {
	t.Helper()
	dirOff := binary.LittleEndian.Uint64(pack[32:40])
	count := int(binary.LittleEndian.Uint32(pack[48:52]))
	for i := 0; i < count; i++ {
		rec := pack[int(dirOff)+i*sectionEntryLen:]
		if binary.LittleEndian.Uint32(rec[:4]) == from {
			binary.LittleEndian.PutUint32(rec[:4], to)
			binary.LittleEndian.PutUint32(rec[4:8], flags)
			return
		}
	}
	t.Fatalf("section %s not found", fourCCString(from))
}

func patchFirstFeatureStatus(t *testing.T, pack []byte, status byte) {
	t.Helper()
	feat := sectionData(t, pack, fourCC("FEAT"))
	if len(feat) < 28 {
		t.Fatal("FEAT section is empty")
	}
	feat[20] = status
}

func patchFirstCoverageScope(t *testing.T, pack []byte, scope uint16) {
	t.Helper()
	covr := sectionData(t, pack, fourCC("COVR"))
	if len(covr) < 12 {
		t.Fatal("COVR section is empty")
	}
	binary.LittleEndian.PutUint16(covr[4:6], scope)
}

func patchFirstCoverageRole(t *testing.T, pack []byte, role byte) {
	t.Helper()
	covr := sectionData(t, pack, fourCC("COVR"))
	if len(covr) < 12 {
		t.Fatal("COVR section is empty")
	}
	covr[6] = role
}

func patchFirstCoverageStatus(t *testing.T, pack []byte, status byte) {
	t.Helper()
	covr := sectionData(t, pack, fourCC("COVR"))
	if len(covr) < 12 {
		t.Fatal("COVR section is empty")
	}
	covr[7] = status
}

func patchFirstKeySetScope(t *testing.T, pack []byte, scope uint16) {
	t.Helper()
	keys := sectionData(t, pack, fourCC("KEYS"))
	if len(keys) < 16 {
		t.Fatal("KEYS section is empty")
	}
	binary.LittleEndian.PutUint16(keys[4:6], scope)
}

func corruptFirstProviderStringRef(t *testing.T, pack []byte) {
	t.Helper()
	cdir := sectionData(t, pack, fourCC("CDIR"))
	chunkOff := binary.LittleEndian.Uint64(cdir[16:24])
	chunkLen := binary.LittleEndian.Uint64(cdir[24:32])
	chunk := pack[chunkOff : chunkOff+chunkLen]
	providerOff := binary.LittleEndian.Uint32(chunk[36:40])
	provider := chunk[providerOff:]
	localesOff := binary.LittleEndian.Uint32(provider[8:12])
	firstRef := provider[localesOff : localesOff+8]
	binary.LittleEndian.PutUint32(firstRef[4:8], ^uint32(0))
}

func sectionData(t *testing.T, pack []byte, kind uint32) []byte {
	t.Helper()
	dirOff := binary.LittleEndian.Uint64(pack[32:40])
	count := int(binary.LittleEndian.Uint32(pack[48:52]))
	for i := 0; i < count; i++ {
		rec := pack[int(dirOff)+i*sectionEntryLen:]
		if binary.LittleEndian.Uint32(rec[:4]) == kind {
			off := binary.LittleEndian.Uint64(rec[8:16])
			n := binary.LittleEndian.Uint64(rec[16:24])
			return pack[off : off+n]
		}
	}
	t.Fatalf("section %s not found", fourCCString(kind))
	return nil
}

func selectedLocalePack(t *testing.T) []byte {
	t.Helper()
	selected, _, err := cldr.SelectBundle(cldr.Builtin(), cldr.Selection{
		Locales:  []string{"de"},
		Features: []cldr.FeatureID{cldr.FeatureNumbersDecimal},
	})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := Build(selected)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func TestBuildIsDeterministic(t *testing.T) {
	a, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(cldr.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("pack build is not deterministic")
	}
}

func BenchmarkOpenPackHashVerified(b *testing.B) {
	pack, err := Build(cldr.Builtin(), WithCodec(CodecZstd))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		bundle, err := FromBytes("bench.cldrpack", pack, WithHashVerification(true))
		if err != nil {
			b.Fatal(err)
		}
		_ = bundle.Close()
	}
}

func BenchmarkWarmLocaleLookup(b *testing.B) {
	pack, err := Build(cldr.Builtin(), WithCodec(CodecZstd))
	if err != nil {
		b.Fatal(err)
	}
	bundle, err := FromBytes("bench.cldrpack", pack)
	if err != nil {
		b.Fatal(err)
	}
	defer bundle.Close()
	if _, ok := bundle.Data().Locale("de-CH"); !ok {
		b.Fatal("missing de-CH")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := bundle.Data().Locale("de-CH"); !ok {
			b.Fatal("missing de-CH")
		}
	}
}
