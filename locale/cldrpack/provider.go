package cldrpack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/nmeilick/go-i18n/locale/cldr"
)

const (
	localeRowLen   = 440
	regionRowLen   = 40
	fractionRowLen = 16
	symbolRowLen   = 16
	bcp47RowLen    = 24
)

type providerData struct {
	data []byte
	pool []byte

	localesOff, localesCount     uint32
	regionsOff, regionsCount     uint32
	fractionsOff, fractionsCount uint32
	symbolsOff, symbolsCount     uint32
	bcp47Off, bcp47Count         uint32
}

func encodeProvider(provider cldr.DataProvider) ([]byte, uint32, error) {
	pool := newStringPool()
	locales := provider.AvailableLocales()
	sort.Strings(locales)
	var localeRows bytes.Buffer
	for _, tag := range locales {
		rec, ok := provider.Locale(tag)
		if !ok {
			continue
		}
		putProviderLocale(&localeRows, pool, rec)
	}
	regions := provider.AvailableRegions()
	sort.Strings(regions)
	var regionRows bytes.Buffer
	for _, region := range regions {
		if rec, ok := provider.RegionDefaults(region); ok {
			putProviderRegion(&regionRows, pool, rec)
		}
	}
	fractions := provider.AvailableCurrencyFractions()
	sort.Slice(fractions, func(i, j int) bool { return fractions[i].Code < fractions[j].Code })
	var fractionRows bytes.Buffer
	for _, rec := range fractions {
		putRef(&fractionRows, pool.ref(rec.Code))
		writeU16(&fractionRows, mustU16(rec.Digits))
		writeU16(&fractionRows, mustU16(rec.CashDigits))
		writeU16(&fractionRows, mustU16(rec.Rounding))
		writeU16(&fractionRows, 0)
	}
	symbols := provider.AvailableCurrencySymbols()
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].Code < symbols[j].Code })
	var symbolRows bytes.Buffer
	for _, rec := range symbols {
		putRef(&symbolRows, pool.ref(rec.Code))
		putRef(&symbolRows, pool.ref(rec.Symbol))
	}
	bcpRecords := []cldr.BCP47TypeRecord{}
	keys := provider.AvailableBCP47Keys()
	sort.Strings(keys)
	for _, key := range keys {
		bcpRecords = append(bcpRecords, provider.BCP47Types(key)...)
	}
	sort.Slice(bcpRecords, func(i, j int) bool {
		a, b := bcpRecords[i], bcpRecords[j]
		return a.Key+"\x00"+a.Type+"\x00"+a.Alias < b.Key+"\x00"+b.Type+"\x00"+b.Alias
	})
	var bcpRows bytes.Buffer
	for _, rec := range bcpRecords {
		putRef(&bcpRows, pool.ref(rec.Key))
		putRef(&bcpRows, pool.ref(rec.Type))
		putRef(&bcpRows, pool.ref(rec.Alias))
	}
	stringBytes := pool.bytes()
	total := providerHdrLen + localeRows.Len() + regionRows.Len() + fractionRows.Len() + symbolRows.Len() + bcpRows.Len() + len(stringBytes)
	out := make([]byte, total)
	copy(out[:4], magicProv)
	binary.LittleEndian.PutUint32(out[4:8], providerHdrLen)
	pos := providerHdrLen
	pos = putTable(out, 8, pos, localeRows.Bytes(), localeRowLen)
	pos = putTable(out, 20, pos, regionRows.Bytes(), regionRowLen)
	pos = putTable(out, 32, pos, fractionRows.Bytes(), fractionRowLen)
	pos = putTable(out, 44, pos, symbolRows.Bytes(), symbolRowLen)
	pos = putTable(out, 56, pos, bcpRows.Bytes(), bcp47RowLen)
	binary.LittleEndian.PutUint32(out[68:72], mustU32(pos))
	binary.LittleEndian.PutUint32(out[72:76], mustU32(len(stringBytes)))
	copy(out[pos:], stringBytes)
	records := mustU32(len(locales) + len(regions) + len(fractions) + len(symbols) + len(bcpRecords))
	return out, records, nil
}

func putTable(out []byte, hdrOff int, pos int, data []byte, elemSize int) int {
	copy(out[pos:], data)
	binary.LittleEndian.PutUint32(out[hdrOff:hdrOff+4], mustU32(pos))
	binary.LittleEndian.PutUint32(out[hdrOff+4:hdrOff+8], mustU32(len(data)/elemSize))
	binary.LittleEndian.PutUint32(out[hdrOff+8:hdrOff+12], mustU32(elemSize))
	return pos + len(data)
}

func putProviderLocale(buf *bytes.Buffer, pool *stringPool, rec cldr.LocaleRecord) {
	putRef(buf, pool.ref(rec.Tag))
	putRef(buf, pool.ref(rec.Parent))
	putRef(buf, pool.ref(rec.NumberingSystem))
	putStringArray(buf, pool, rec.DateFormats[:])
	putStringArray(buf, pool, rec.TimeFormats[:])
	putStringArray(buf, pool, rec.DateTimeFormats[:])
	putStringArray(buf, pool, rec.MonthsWide[:])
	putStringArray(buf, pool, rec.MonthsAbbr[:])
	putStringArray(buf, pool, rec.WeekdaysWide[:])
	putStringArray(buf, pool, rec.WeekdaysAbbr[:])
	putRef(buf, pool.ref(rec.CurrencyPattern))
	putRef(buf, pool.ref(rec.Accounting))
}

func putProviderRegion(buf *bytes.Buffer, pool *stringPool, rec cldr.RegionDefaults) {
	putRef(buf, pool.ref(rec.Region))
	putRef(buf, pool.ref(rec.Currency))
	putRef(buf, pool.ref(rec.MeasurementSystem))
	putRef(buf, pool.ref(rec.FirstDay))
	putRef(buf, pool.ref(rec.TimeZone))
}

func putStringArray(buf *bytes.Buffer, pool *stringPool, values []string) {
	for _, value := range values {
		putRef(buf, pool.ref(value))
	}
}

func parseProvider(data []byte) (providerData, error) {
	if len(data) < providerHdrLen || string(data[:4]) != magicProv {
		return providerData{}, fmt.Errorf("open cldrpack: invalid provider payload")
	}
	if binary.LittleEndian.Uint32(data[4:8]) != providerHdrLen {
		return providerData{}, fmt.Errorf("open cldrpack: unsupported provider header length")
	}
	p := providerData{data: data}
	var err error
	if p.localesOff, p.localesCount, err = tableHeader(data, 8, localeRowLen); err != nil {
		return providerData{}, err
	}
	if p.regionsOff, p.regionsCount, err = tableHeader(data, 20, regionRowLen); err != nil {
		return providerData{}, err
	}
	if p.fractionsOff, p.fractionsCount, err = tableHeader(data, 32, fractionRowLen); err != nil {
		return providerData{}, err
	}
	if p.symbolsOff, p.symbolsCount, err = tableHeader(data, 44, symbolRowLen); err != nil {
		return providerData{}, err
	}
	if p.bcp47Off, p.bcp47Count, err = tableHeader(data, 56, bcp47RowLen); err != nil {
		return providerData{}, err
	}
	poolOff := binary.LittleEndian.Uint32(data[68:72])
	poolLen := binary.LittleEndian.Uint32(data[72:76])
	if !rangeOK(uint64(poolOff), uint64(poolLen), uint64(len(data))) {
		return providerData{}, fmt.Errorf("open cldrpack: provider string pool outside payload")
	}
	poolStart, ok := u32ToInt(poolOff)
	if !ok {
		return providerData{}, fmt.Errorf("open cldrpack: provider string pool offset too large")
	}
	poolLength, ok := u32ToInt(poolLen)
	if !ok || poolStart > len(data)-poolLength {
		return providerData{}, fmt.Errorf("open cldrpack: provider string pool too large")
	}
	p.pool = data[poolStart : poolStart+poolLength]
	if err := p.validateRefs(); err != nil {
		return providerData{}, err
	}
	return p, nil
}

func tableHeader(data []byte, off int, elemSize int) (uint32, uint32, error) {
	tableOff := binary.LittleEndian.Uint32(data[off : off+4])
	count := binary.LittleEndian.Uint32(data[off+4 : off+8])
	gotSize := binary.LittleEndian.Uint32(data[off+8 : off+12])
	if gotSize != mustU32(elemSize) {
		return 0, 0, fmt.Errorf("open cldrpack: provider table elem size mismatch")
	}
	tableLen, ok := checkedMulU64(uint64(count), mustU64(elemSize))
	if !ok || !rangeOK(uint64(tableOff), tableLen, uint64(len(data))) {
		return 0, 0, fmt.Errorf("open cldrpack: provider table outside payload")
	}
	return tableOff, count, nil
}

func (p providerData) localeExact(tag string) (cldr.LocaleRecord, bool) {
	i := sort.Search(int(p.localesCount), func(i int) bool {
		return p.localeTag(i) >= tag
	})
	if i < int(p.localesCount) && p.localeTag(i) == tag {
		return p.decodeLocale(i), true
	}
	return cldr.LocaleRecord{}, false
}

func (p providerData) localeTag(i int) string {
	row := p.row(p.localesOff, i, localeRowLen)
	return p.ref(row[0:8])
}

func (p providerData) decodeLocale(i int) cldr.LocaleRecord {
	row := p.row(p.localesOff, i, localeRowLen)
	pos := 0
	next := func() string {
		s := p.ref(row[pos : pos+8])
		pos += 8
		return s
	}
	rec := cldr.LocaleRecord{Tag: next(), Parent: next(), NumberingSystem: next()}
	readArray4 := func(dst *[4]string) {
		for i := range dst {
			dst[i] = next()
		}
	}
	readArray12 := func(dst *[12]string) {
		for i := range dst {
			dst[i] = next()
		}
	}
	readArray7 := func(dst *[7]string) {
		for i := range dst {
			dst[i] = next()
		}
	}
	readArray4(&rec.DateFormats)
	readArray4(&rec.TimeFormats)
	readArray4(&rec.DateTimeFormats)
	readArray12(&rec.MonthsWide)
	readArray12(&rec.MonthsAbbr)
	readArray7(&rec.WeekdaysWide)
	readArray7(&rec.WeekdaysAbbr)
	rec.CurrencyPattern = next()
	rec.Accounting = next()
	return rec
}

func (p providerData) region(region string) (cldr.RegionDefaults, bool) {
	region = strings.ToUpper(strings.TrimSpace(region))
	i := sort.Search(int(p.regionsCount), func(i int) bool {
		return p.regionCode(i) >= region
	})
	if i < int(p.regionsCount) && p.regionCode(i) == region {
		return p.decodeRegion(i), true
	}
	i = sort.Search(int(p.regionsCount), func(i int) bool {
		return p.regionCode(i) >= "001"
	})
	if i < int(p.regionsCount) && p.regionCode(i) == "001" {
		return p.decodeRegion(i), true
	}
	return cldr.RegionDefaults{}, false
}

func (p providerData) regionCode(i int) string {
	row := p.row(p.regionsOff, i, regionRowLen)
	return p.ref(row[0:8])
}

func (p providerData) decodeRegion(i int) cldr.RegionDefaults {
	row := p.row(p.regionsOff, i, regionRowLen)
	return cldr.RegionDefaults{
		Region:            p.ref(row[0:8]),
		Currency:          p.ref(row[8:16]),
		MeasurementSystem: p.ref(row[16:24]),
		FirstDay:          p.ref(row[24:32]),
		TimeZone:          p.ref(row[32:40]),
	}
}

func (p providerData) fraction(code string) cldr.CurrencyFraction {
	code = strings.ToUpper(strings.TrimSpace(code))
	i := sort.Search(int(p.fractionsCount), func(i int) bool {
		return p.fractionCode(i) >= code
	})
	if i < int(p.fractionsCount) && p.fractionCode(i) == code {
		row := p.row(p.fractionsOff, i, fractionRowLen)
		return cldr.CurrencyFraction{
			Code:       p.ref(row[0:8]),
			Digits:     int(binary.LittleEndian.Uint16(row[8:10])),
			CashDigits: int(binary.LittleEndian.Uint16(row[10:12])),
			Rounding:   int(binary.LittleEndian.Uint16(row[12:14])),
		}
	}
	return cldr.CurrencyFraction{Code: code, Digits: 2, CashDigits: 2}
}

func (p providerData) fractionCode(i int) string {
	return p.ref(p.row(p.fractionsOff, i, fractionRowLen)[0:8])
}

func (p providerData) symbol(code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	i := sort.Search(int(p.symbolsCount), func(i int) bool {
		return p.symbolCode(i) >= code
	})
	if i < int(p.symbolsCount) && p.symbolCode(i) == code {
		row := p.row(p.symbolsOff, i, symbolRowLen)
		return p.ref(row[8:16]), true
	}
	return "", false
}

func (p providerData) symbolCode(i int) string {
	return p.ref(p.row(p.symbolsOff, i, symbolRowLen)[0:8])
}

func (p providerData) bcp47(key string) []cldr.BCP47TypeRecord {
	key = strings.TrimSpace(key)
	i := sort.Search(int(p.bcp47Count), func(i int) bool {
		return p.bcp47Key(i) >= key
	})
	out := []cldr.BCP47TypeRecord{}
	for ; i < int(p.bcp47Count) && p.bcp47Key(i) == key; i++ {
		row := p.row(p.bcp47Off, i, bcp47RowLen)
		out = append(out, cldr.BCP47TypeRecord{
			Key:   p.ref(row[0:8]),
			Type:  p.ref(row[8:16]),
			Alias: p.ref(row[16:24]),
		})
	}
	return out
}

func (p providerData) bcp47Key(i int) string {
	return p.ref(p.row(p.bcp47Off, i, bcp47RowLen)[0:8])
}

func (p providerData) regions() []string {
	out := make([]string, int(p.regionsCount))
	for i := range out {
		out[i] = p.regionCode(i)
	}
	return out
}

func (p providerData) fractions() []cldr.CurrencyFraction {
	out := make([]cldr.CurrencyFraction, int(p.fractionsCount))
	for i := range out {
		row := p.row(p.fractionsOff, i, fractionRowLen)
		out[i] = cldr.CurrencyFraction{
			Code:       p.ref(row[0:8]),
			Digits:     int(binary.LittleEndian.Uint16(row[8:10])),
			CashDigits: int(binary.LittleEndian.Uint16(row[10:12])),
			Rounding:   int(binary.LittleEndian.Uint16(row[12:14])),
		}
	}
	return out
}

func (p providerData) symbols() []cldr.CurrencySymbolRecord {
	out := make([]cldr.CurrencySymbolRecord, int(p.symbolsCount))
	for i := range out {
		row := p.row(p.symbolsOff, i, symbolRowLen)
		out[i] = cldr.CurrencySymbolRecord{Code: p.ref(row[0:8]), Symbol: p.ref(row[8:16])}
	}
	return out
}

func (p providerData) bcp47Keys() []string {
	keys := []string{}
	var last string
	for i := 0; i < int(p.bcp47Count); i++ {
		key := p.bcp47Key(i)
		if key != last {
			keys = append(keys, key)
			last = key
		}
	}
	return keys
}

func (p providerData) row(off uint32, i int, n int) []byte {
	start := int(off) + i*n
	return p.data[start : start+n]
}

func (p providerData) ref(b []byte) string {
	off := binary.LittleEndian.Uint32(b[:4])
	n := binary.LittleEndian.Uint32(b[4:8])
	if !rangeOK(uint64(off), uint64(n), uint64(len(p.pool))) {
		return ""
	}
	return string(p.pool[off : off+n])
}

func (p providerData) validateRefs() error {
	tables := []struct {
		name     string
		off      uint32
		count    uint32
		rowLen   int
		refSlots int
	}{
		{name: "locale", off: p.localesOff, count: p.localesCount, rowLen: localeRowLen, refSlots: localeRowLen / 8},
		{name: "region", off: p.regionsOff, count: p.regionsCount, rowLen: regionRowLen, refSlots: regionRowLen / 8},
		{name: "fraction", off: p.fractionsOff, count: p.fractionsCount, rowLen: fractionRowLen, refSlots: 1},
		{name: "symbol", off: p.symbolsOff, count: p.symbolsCount, rowLen: symbolRowLen, refSlots: 2},
		{name: "bcp47", off: p.bcp47Off, count: p.bcp47Count, rowLen: bcp47RowLen, refSlots: 3},
	}
	for _, table := range tables {
		count, ok := u32ToInt(table.count)
		if !ok {
			return fmt.Errorf("open cldrpack: provider %s table too large", table.name)
		}
		for i := 0; i < count; i++ {
			row := p.row(table.off, i, table.rowLen)
			for slot := 0; slot < table.refSlots; slot++ {
				if _, err := p.checkedRef(row[slot*8 : slot*8+8]); err != nil {
					return fmt.Errorf("open cldrpack: provider %s string ref %d/%d: %w", table.name, i, slot, err)
				}
			}
		}
	}
	return nil
}

func (p providerData) checkedRef(b []byte) (string, error) {
	off := binary.LittleEndian.Uint32(b[:4])
	n := binary.LittleEndian.Uint32(b[4:8])
	if !rangeOK(uint64(off), uint64(n), uint64(len(p.pool))) {
		return "", fmt.Errorf("string ref outside pool")
	}
	return string(p.pool[off : off+n]), nil
}
