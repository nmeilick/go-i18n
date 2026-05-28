package cldrpack

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/nmeilick/go-i18n/locale/cldr"
)

const maxIntValue = int(^uint(0) >> 1)

type section struct {
	kind     uint32
	offset   uint64
	length   uint64
	elemSize uint32
	count    uint32
	data     []byte
}

type stringPool struct {
	buf   bytes.Buffer
	index map[string]stringRef
}

type stringRef struct {
	off uint32
	len uint32
}

func newStringPool() *stringPool {
	return &stringPool{index: map[string]stringRef{}}
}

func (p *stringPool) ref(s string) stringRef {
	if ref, ok := p.index[s]; ok {
		return ref
	}
	ref := stringRef{off: mustU32(p.buf.Len()), len: mustU32(len(s))}
	p.buf.WriteString(s)
	p.index[s] = ref
	return ref
}

func (p *stringPool) bytes() []byte {
	return append([]byte(nil), p.buf.Bytes()...)
}

func encodeMeta(info cldr.Info, pool *stringPool) []byte {
	values := map[string]string{
		"id":                       info.ID,
		"name":                     info.Name,
		"mode":                     info.Mode,
		"cldr":                     info.Versions.CLDR,
		"unicode":                  info.Versions.Unicode,
		"tzdb":                     info.Versions.TZDB,
		"generator":                info.Versions.Generator,
		"source_identity":          info.Versions.SourceIdentity,
		"source_digest":            info.Versions.SourceDigest,
		"digest":                   info.Digest,
		"license":                  info.Versions.License,
		"provider_api_major":       strconv.Itoa(cldr.ProviderAPIMajor),
		"provider_api_minor":       strconv.Itoa(cldr.ProviderAPIMinor),
		"feature_registry_major":   strconv.Itoa(cldr.FeatureRegistryMajor),
		"feature_registry_minor":   strconv.Itoa(cldr.FeatureRegistryMinor),
		"codec_requirements":       "raw,zstd",
		"cldrpack_schema_version":  "1.0",
		"cldrpack_integrity_hash":  "sha256",
		"cldrpack_signature_algs":  "ed25519",
		"cldrpack_payload_formats": "provider-v1",
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	for _, key := range keys {
		putRef(&out, pool.ref(key))
		putRef(&out, pool.ref(values[key]))
	}
	return out.Bytes()
}

func encodeFeatures(info cldr.Info, coverage []cldr.Coverage, pool *stringPool) []byte {
	ids := featureIDMap(coverage)
	features := make([]cldr.FeatureID, 0, len(ids))
	for feature := range ids {
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i] < features[j] })
	var out bytes.Buffer
	for _, feature := range features {
		writeU32(&out, ids[feature])
		putRef(&out, pool.ref(string(feature)))
		putRef(&out, pool.ref(string(feature)))
		out.WriteByte(statusToWire(statusForFeature(feature, coverage)))
		out.Write([]byte{0, 0, 0})
		writeU32(&out, 0)
	}
	return out.Bytes()
}

func encodeLocales(data cldr.DataProvider, pool *stringPool) []byte {
	locales := data.AvailableLocales()
	sort.Strings(locales)
	var out bytes.Buffer
	for i, tag := range locales {
		writeU32(&out, mustU32(i+1))
		putRef(&out, pool.ref(tag))
		parentID := nilID
		if parent, ok := data.Parent(tag); ok && parent != "" {
			if j := sort.SearchStrings(locales, parent); j < len(locales) && locales[j] == parent {
				parentID = mustU32(j + 1)
			}
		}
		writeU32(&out, parentID)
		writeU32(&out, mustU32(i+1))
		writeU32(&out, nilID)
		writeU32(&out, nilID)
		writeU32(&out, 0)
	}
	return out.Bytes()
}

func normalizeBuildCoverage(coverage []cldr.Coverage) ([]cldr.Coverage, error) {
	out := make([]cldr.Coverage, 0, len(coverage))
	for _, rec := range coverage {
		if rec.Feature == "" {
			return nil, fmt.Errorf("build cldrpack: coverage feature is required")
		}
		if rec.Scope == "" {
			rec.Scope = cldr.ScopeGlobal
		}
		if rec.Role == "" {
			rec.Role = cldr.RoleAuthoritative
		}
		if rec.Status == "" {
			rec.Status = cldr.StatusDataAvailable
		}
		if rec.All {
			rec.Keys = nil
		} else {
			rec.Keys = sortedUniqueStrings(rec.Keys)
			if len(rec.Keys) == 0 {
				continue
			}
		}
		out = append(out, rec)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("build cldrpack: bundle has no coverage")
	}
	sortCoverage(out)
	return out, nil
}

func encodeCoverageAndKeySets(coverage []cldr.Coverage, pool *stringPool) ([]byte, []byte) {
	coverage = append([]cldr.Coverage(nil), coverage...)
	sortCoverage(coverage)
	featureIDs := featureIDMap(coverage)
	var covr bytes.Buffer
	var keys bytes.Buffer
	keySetIDs := map[string]uint32{}
	for _, rec := range coverage {
		writeU32(&covr, featureIDs[rec.Feature])
		writeU16(&covr, scopeToWire(rec.Scope))
		covr.WriteByte(roleToWire(rec.Role))
		covr.WriteByte(statusToWire(rec.Status))
		if rec.All || len(rec.Keys) == 0 {
			writeU32(&covr, nilID)
		} else {
			idKey := string(rec.Scope) + "\x00" + strings.Join(rec.Keys, "\x00")
			setID, ok := keySetIDs[idKey]
			if !ok {
				setID = mustU32(len(keySetIDs) + 1)
				keySetIDs[idKey] = setID
				for _, key := range rec.Keys {
					writeU32(&keys, setID)
					writeU16(&keys, scopeToWire(rec.Scope))
					writeU16(&keys, 0)
					putRef(&keys, pool.ref(key))
				}
			}
			writeU32(&covr, setID)
		}
	}
	return covr.Bytes(), keys.Bytes()
}

func sortCoverage(coverage []cldr.Coverage) {
	sort.SliceStable(coverage, func(i, j int) bool {
		a, b := coverage[i], coverage[j]
		return string(a.Feature)+"\x00"+string(a.Scope)+"\x00"+string(a.Role)+"\x00"+strings.Join(a.Keys, "\x00") <
			string(b.Feature)+"\x00"+string(b.Scope)+"\x00"+string(b.Role)+"\x00"+strings.Join(b.Keys, "\x00")
	})
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func encodeRoutes(coverage []cldr.Coverage) []byte {
	featureIDs := featureIDMap(coverage)
	var out bytes.Buffer
	for _, rec := range coverage {
		writeU32(&out, featureIDs[rec.Feature])
		writeU16(&out, scopeToWire(rec.Scope))
		writeU16(&out, 0)
		writeU32(&out, nilID)
		writeU32(&out, nilID)
		writeU32(&out, nilID)
		writeU32(&out, nilID)
		writeU32(&out, nilID)
		writeU32(&out, 1)
	}
	return out.Bytes()
}

func featureIDMap(coverage []cldr.Coverage) map[cldr.FeatureID]uint32 {
	seen := map[cldr.FeatureID]bool{}
	for _, rec := range coverage {
		seen[rec.Feature] = true
	}
	features := make([]cldr.FeatureID, 0, len(seen))
	for feature := range seen {
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i] < features[j] })
	out := map[cldr.FeatureID]uint32{}
	for i, feature := range features {
		out[feature] = mustU32(i + 1)
	}
	return out
}

func statusForFeature(feature cldr.FeatureID, coverage []cldr.Coverage) cldr.CapabilityStatus {
	for _, rec := range coverage {
		if rec.Feature == feature && rec.Status != "" {
			return rec.Status
		}
	}
	return cldr.StatusDataAvailable
}

func encodeChunkDirectory(offset, packedLen, unpackedLen uint64, records uint32, codec Codec) []byte {
	var out bytes.Buffer
	writeU32(&out, 1)
	writeU32(&out, 1)
	writeU32(&out, formatProvider)
	writeU16(&out, uint16(codec))
	writeU16(&out, 0)
	writeU64(&out, offset)
	writeU64(&out, packedLen)
	writeU64(&out, unpackedLen)
	writeU32(&out, records)
	writeU32(&out, 0)
	return out.Bytes()
}

func wrapChunk(provider []byte, records uint32) []byte {
	buf := make([]byte, chunkHeaderLen+len(provider))
	copy(buf[:4], magicChunk)
	binary.LittleEndian.PutUint32(buf[4:8], formatProvider)
	binary.LittleEndian.PutUint32(buf[8:12], records)
	binary.LittleEndian.PutUint16(buf[12:14], chunkHeaderLen)
	binary.LittleEndian.PutUint32(buf[36:40], mustU32(chunkHeaderLen))
	binary.LittleEndian.PutUint32(buf[40:44], mustU32(len(provider)))
	copy(buf[chunkHeaderLen:], provider)
	return buf
}

func unwrapChunk(payload []byte) ([]byte, error) {
	if len(payload) < chunkHeaderLen {
		return nil, fmt.Errorf("open cldrpack: truncated chunk header")
	}
	if string(payload[:4]) != magicChunk {
		return nil, fmt.Errorf("open cldrpack: wrong chunk magic")
	}
	if binary.LittleEndian.Uint32(payload[4:8]) != formatProvider {
		return nil, fmt.Errorf("open cldrpack: unsupported chunk format")
	}
	if binary.LittleEndian.Uint16(payload[12:14]) != chunkHeaderLen {
		return nil, fmt.Errorf("open cldrpack: unsupported chunk header length")
	}
	if !allZero(payload[16:20]) || !allZero(payload[44:48]) {
		return nil, fmt.Errorf("open cldrpack: chunk reserved bytes are non-zero")
	}
	off := binary.LittleEndian.Uint32(payload[36:40])
	n := binary.LittleEndian.Uint32(payload[40:44])
	if !rangeOK(uint64(off), uint64(n), uint64(len(payload))) {
		return nil, fmt.Errorf("open cldrpack: chunk data outside payload")
	}
	return payload[off : off+n], nil
}

func writeHeader(b []byte, sectionDirOff, sectionDirLen uint64, sectionCount uint32, bodyLen, integrityOff, integrityLen uint64) {
	copy(b[:8], magicHeader)
	binary.LittleEndian.PutUint32(b[8:12], endianMarker)
	binary.LittleEndian.PutUint16(b[12:14], schemaMajor)
	binary.LittleEndian.PutUint16(b[14:16], schemaMinor)
	binary.LittleEndian.PutUint32(b[16:20], headerLen)
	binary.LittleEndian.PutUint16(b[24:26], cldr.ProviderAPIMajor)
	binary.LittleEndian.PutUint16(b[26:28], cldr.ProviderAPIMinor)
	binary.LittleEndian.PutUint16(b[28:30], cldr.FeatureRegistryMajor)
	binary.LittleEndian.PutUint16(b[30:32], cldr.FeatureRegistryMinor)
	binary.LittleEndian.PutUint64(b[32:40], sectionDirOff)
	binary.LittleEndian.PutUint64(b[40:48], sectionDirLen)
	binary.LittleEndian.PutUint32(b[48:52], sectionCount)
	binary.LittleEndian.PutUint64(b[56:64], bodyLen)
	binary.LittleEndian.PutUint64(b[64:72], integrityOff)
	binary.LittleEndian.PutUint64(b[72:80], integrityLen)
}

func writeSectionDirectory(dst []byte, sections []section) {
	for i, sec := range sections {
		off := i * sectionEntryLen
		binary.LittleEndian.PutUint32(dst[off:], sec.kind)
		binary.LittleEndian.PutUint32(dst[off+4:], sectionFlags(sec))
		binary.LittleEndian.PutUint64(dst[off+8:], sec.offset)
		binary.LittleEndian.PutUint64(dst[off+16:], sec.length)
		binary.LittleEndian.PutUint32(dst[off+24:], sec.elemSize)
		binary.LittleEndian.PutUint32(dst[off+28:], sec.count)
	}
}

func sectionFlags(sec section) uint32 {
	flags := uint32(sectionFlagRequired)
	if sec.elemSize > 0 {
		flags |= sectionFlagSorted | sectionFlagFixedEntry
	}
	return flags
}

func integrityLength(signatures []signatureBuild) int {
	n := 4 + 2 + 2 + 2 + sha256.Size + 2
	for _, sig := range signatures {
		n += 2 + 2 + 4 + 4 + len(sig.keyID) + len(sig.meta) + ed25519.SignatureSize
	}
	return n
}

func encodeIntegrity(digest []byte, signatures []signatureBuild) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString(magicInt)
	writeU16(&out, 1)
	writeU16(&out, hashSHA256)
	writeU16(&out, mustU16(len(digest)))
	out.Write(digest)
	writeU16(&out, mustU16(len(signatures)))
	for _, sig := range signatures {
		signature := ed25519.Sign(sig.key, signatureMessage(hashSHA256, digest, sig.meta))
		writeU16(&out, sigEd25519)
		writeU16(&out, mustU16(len(sig.keyID)))
		writeU32(&out, mustU32(len(sig.meta)))
		writeU32(&out, mustU32(len(signature)))
		out.WriteString(sig.keyID)
		out.Write(sig.meta)
		out.Write(signature)
	}
	return out.Bytes(), nil
}

type signatureRecord struct {
	alg  uint16
	key  string
	meta []byte
	sig  []byte
}

func parseIntegrity(data []byte, limits Limits) ([]byte, []signatureRecord, error) {
	if limits.MaxIntegrityTrailerBytes > 0 && int64(len(data)) > limits.MaxIntegrityTrailerBytes {
		return nil, nil, fmt.Errorf("open cldrpack: integrity trailer exceeds limit")
	}
	if len(data) < 4+2+2+2+sha256.Size+2 || string(data[:4]) != magicInt {
		return nil, nil, fmt.Errorf("open cldrpack: invalid integrity trailer")
	}
	version := binary.LittleEndian.Uint16(data[4:6])
	alg := binary.LittleEndian.Uint16(data[6:8])
	digestLen := binary.LittleEndian.Uint16(data[8:10])
	if version != 1 || alg != hashSHA256 || digestLen != sha256.Size {
		return nil, nil, fmt.Errorf("open cldrpack: unsupported integrity algorithm")
	}
	pos := 10
	digest := append([]byte(nil), data[pos:pos+sha256.Size]...)
	pos += sha256.Size
	sigCount := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
	pos += 2
	out := make([]signatureRecord, 0, sigCount)
	for i := 0; i < sigCount; i++ {
		if pos+12 > len(data) {
			return nil, nil, fmt.Errorf("open cldrpack: truncated signature record")
		}
		rec := signatureRecord{
			alg: binary.LittleEndian.Uint16(data[pos : pos+2]),
		}
		keyLen := int(binary.LittleEndian.Uint16(data[pos+2 : pos+4]))
		metaLen, ok := u32ToInt(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		if !ok {
			return nil, nil, fmt.Errorf("open cldrpack: signature metadata too large")
		}
		sigLen, ok := u32ToInt(binary.LittleEndian.Uint32(data[pos+8 : pos+12]))
		if !ok {
			return nil, nil, fmt.Errorf("open cldrpack: signature too large")
		}
		pos += 12
		totalLen, ok := checkedAddInt(keyLen, metaLen, sigLen)
		if !ok {
			return nil, nil, fmt.Errorf("open cldrpack: signature record too large")
		}
		if limits.MaxSignatureBytes > 0 && int64(totalLen) > limits.MaxSignatureBytes {
			return nil, nil, fmt.Errorf("open cldrpack: signature record exceeds limit")
		}
		end, ok := checkedAddInt(pos, totalLen)
		if !ok || end > len(data) {
			return nil, nil, fmt.Errorf("open cldrpack: truncated signature payload")
		}
		rec.key = string(data[pos : pos+keyLen])
		pos += keyLen
		rec.meta = append([]byte(nil), data[pos:pos+metaLen]...)
		pos += metaLen
		rec.sig = append([]byte(nil), data[pos:pos+sigLen]...)
		pos += sigLen
		out = append(out, rec)
	}
	if pos != len(data) {
		return nil, nil, fmt.Errorf("open cldrpack: trailing integrity bytes")
	}
	return digest, out, nil
}

func verifySignatures(digest []byte, signatures []signatureRecord, keys map[string]ed25519.PublicKey) error {
	for _, rec := range signatures {
		if rec.alg != sigEd25519 {
			continue
		}
		key, ok := keys[rec.key]
		if !ok {
			continue
		}
		if ed25519.Verify(key, signatureMessage(hashSHA256, digest, rec.meta), rec.sig) {
			return nil
		}
	}
	return fmt.Errorf("open cldrpack: signature verification failed")
}

func signatureMessage(hashAlg uint16, digest, signedMeta []byte) []byte {
	var out bytes.Buffer
	out.WriteString("go-i18n cldrpack signature v1\x00")
	writeU16(&out, hashAlg)
	out.Write(digest)
	out.Write(signedMeta)
	return out.Bytes()
}

func hashRange(src source, n int64) ([]byte, error) {
	h := sha256.New()
	buf := make([]byte, 64*1024)
	var off int64
	for off < n {
		want := int64(len(buf))
		if rem := n - off; rem < want {
			want = rem
		}
		chunkLen, ok := i64ToInt(want)
		if !ok {
			return nil, fmt.Errorf("open cldrpack: hash chunk too large")
		}
		read, err := src.ReadAt(buf[:chunkLen], off)
		if err != nil && !(err == io.EOF && int64(read) == want) {
			return nil, err
		}
		h.Write(buf[:read])
		off += int64(read)
	}
	return h.Sum(nil), nil
}

func readAt(src source, off, n int64) ([]byte, error) {
	size := src.Size()
	if n < 0 || off < 0 || off > size || n > size-off {
		return nil, fmt.Errorf("open cldrpack: read outside source")
	}
	length, ok := i64ToInt(n)
	if !ok {
		return nil, fmt.Errorf("open cldrpack: read size too large")
	}
	if b, ok := src.Slice(off, n); ok {
		return append([]byte(nil), b...), nil
	}
	out := make([]byte, length)
	if _, err := src.ReadAt(out, off); err != nil {
		return nil, err
	}
	return out, nil
}

func readAtU64(src source, off, n uint64) ([]byte, error) {
	iOff, ok := u64ToI64(off)
	if !ok {
		return nil, fmt.Errorf("open cldrpack: read offset too large")
	}
	iN, ok := u64ToI64(n)
	if !ok {
		return nil, fmt.Errorf("open cldrpack: read size too large")
	}
	return readAt(src, iOff, iN)
}

func readStringRef(pool []byte, ref []byte) (string, error) {
	off := binary.LittleEndian.Uint32(ref[:4])
	n := binary.LittleEndian.Uint32(ref[4:8])
	if !rangeOK(uint64(off), uint64(n), uint64(len(pool))) {
		return "", fmt.Errorf("open cldrpack: string ref outside pool")
	}
	return string(pool[off : off+n]), nil
}

func putRef(buf *bytes.Buffer, ref stringRef) {
	writeU32(buf, ref.off)
	writeU32(buf, ref.len)
}

func writeU16(buf *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	buf.Write(b[:])
}

func writeU32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func writeU64(buf *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	buf.Write(b[:])
}

func fourCC(s string) uint32 {
	if len(s) != 4 {
		panic("fourCC requires four bytes")
	}
	return uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
}

func fourCCString(v uint32) string {
	return string([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

func alignU64(v uint64) uint64 {
	if v%8 == 0 {
		return v
	}
	return v + (8 - v%8)
}

func rangeOK(off, n, size uint64) bool {
	return off <= size && n <= size-off
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func residentBytes(entries map[uint32]sectionEntry) int64 {
	var n int64
	for _, entry := range entries {
		length, ok := u64ToI64(entry.length)
		if !ok || n > int64(maxIntValue)-length {
			return int64(maxIntValue)
		}
		n += length
	}
	return n
}

func u64ToI64(v uint64) (int64, bool) {
	if v > uint64(^uint64(0)>>1) {
		return 0, false
	}
	return int64(v), true
}

func i64ToU64(v int64) (uint64, bool) {
	if v < 0 {
		return 0, false
	}
	return uint64(v), true
}

func i64ToInt(v int64) (int, bool) {
	if v < 0 || v > int64(maxIntValue) {
		return 0, false
	}
	return int(v), true
}

func u32ToInt(v uint32) (int, bool) {
	if uint64(v) > uint64(maxIntValue) {
		return 0, false
	}
	return int(v), true
}

func checkedAddInt(values ...int) (int, bool) {
	var out int
	for _, v := range values {
		if v < 0 || out > maxIntValue-v {
			return 0, false
		}
		out += v
	}
	return out, true
}

func checkedMulU64(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > ^uint64(0)/b {
		return 0, false
	}
	return a * b, true
}

func mustU32(v int) uint32 {
	if v < 0 || uint64(v) > uint64(^uint32(0)) {
		panic("cldrpack internal uint32 overflow")
	}
	// #nosec G115 -- the bounds check above proves v fits uint32.
	return uint32(v)
}

func mustU16(v int) uint16 {
	if v < 0 || v > int(^uint16(0)) {
		panic("cldrpack internal uint16 overflow")
	}
	// #nosec G115 -- the bounds check above proves v fits uint16.
	return uint16(v)
}

func mustU64(v int) uint64 {
	if v < 0 {
		panic("cldrpack internal uint64 overflow")
	}
	// #nosec G115 -- the bounds check above proves v is non-negative.
	return uint64(v)
}

func statusToWire(status cldr.CapabilityStatus) byte {
	switch status {
	case cldr.StatusUnsupported:
		return 0
	case cldr.StatusDataAvailable, "":
		return 1
	case cldr.StatusExperimental:
		return 2
	case cldr.StatusImplemented:
		return 3
	case cldr.StatusConformant:
		return 4
	default:
		return 1
	}
}

func statusFromWire(v byte) (cldr.CapabilityStatus, bool) {
	switch v {
	case 0:
		return cldr.StatusUnsupported, true
	case 1:
		return cldr.StatusDataAvailable, true
	case 2:
		return cldr.StatusExperimental, true
	case 3:
		return cldr.StatusImplemented, true
	case 4:
		return cldr.StatusConformant, true
	default:
		return "", false
	}
}

func roleToWire(role cldr.CoverageRole) byte {
	if role == cldr.RoleDependency {
		return 1
	}
	return 0
}

func roleFromWire(v byte) (cldr.CoverageRole, bool) {
	switch v {
	case 0:
		return cldr.RoleAuthoritative, true
	case 1:
		return cldr.RoleDependency, true
	default:
		return "", false
	}
}

func scopeToWire(scope cldr.ScopeKind) uint16 {
	switch scope {
	case cldr.ScopeGlobal, "":
		return 0
	case cldr.ScopeLocale:
		return 1
	case cldr.ScopeTerritory:
		return 2
	case cldr.ScopeCurrency:
		return 3
	case cldr.ScopeUnit:
		return 4
	case cldr.ScopeCalendar:
		return 5
	case cldr.ScopeTimeZone:
		return 6
	case cldr.ScopeMetaZone:
		return 7
	case cldr.ScopeNumberingSystem:
		return 8
	case cldr.ScopeCollation:
		return 9
	case cldr.ScopeFeaturePrivate:
		return 255
	default:
		return 255
	}
}

func scopeFromWire(v uint16) (cldr.ScopeKind, bool) {
	switch v {
	case 0:
		return cldr.ScopeGlobal, true
	case 1:
		return cldr.ScopeLocale, true
	case 2:
		return cldr.ScopeTerritory, true
	case 3:
		return cldr.ScopeCurrency, true
	case 4:
		return cldr.ScopeUnit, true
	case 5:
		return cldr.ScopeCalendar, true
	case 6:
		return cldr.ScopeTimeZone, true
	case 7:
		return cldr.ScopeMetaZone, true
	case 8:
		return cldr.ScopeNumberingSystem, true
	case 9:
		return cldr.ScopeCollation, true
	case 255:
		return cldr.ScopeFeaturePrivate, true
	default:
		return "", false
	}
}
