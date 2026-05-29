package cldrpack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nmeilick/go-i18n/locale/cldr"
)

const (
	stringRefLen               = 8
	providerPrefixLen          = 8
	providerTableHeaderLen     = 12
	providerStringPoolEntryLen = 8
	providerStringPoolOff      = providerPrefixLen + int(providerTableCount)*providerTableHeaderLen
	providerStringPoolLenOff   = providerStringPoolOff + 4

	providerLocaleStringRefs          = 57
	providerRegionStringRefs          = 5
	providerFractionStringRefs        = 1
	providerSymbolStringRefs          = 4
	providerBCP47StringRefs           = 3
	providerListPatternStringRefs     = 7
	providerUnitPatternStringRefs     = 5
	providerCompactPatternStringRefs  = 5
	providerRelativePatternStringRefs = 6
	providerRelativeSpecialStringRefs = 5
	providerIntervalPatternStringRefs = 4
	providerDisplayNameStringRefs     = 4

	localeRowLen          = providerLocaleStringRefs * stringRefLen
	regionRowLen          = providerRegionStringRefs * stringRefLen
	fractionRowLen        = providerFractionStringRefs*stringRefLen + 8
	symbolRowLen          = providerSymbolStringRefs * stringRefLen
	bcp47RowLen           = providerBCP47StringRefs * stringRefLen
	listPatternRowLen     = providerListPatternStringRefs * stringRefLen
	unitPatternRowLen     = providerUnitPatternStringRefs * stringRefLen
	compactPatternRowLen  = providerCompactPatternStringRefs * stringRefLen
	relativePatternRowLen = providerRelativePatternStringRefs * stringRefLen
	relativeSpecialRowLen = providerRelativeSpecialStringRefs * stringRefLen
	intervalPatternRowLen = providerIntervalPatternStringRefs * stringRefLen
	displayNameRowLen     = providerDisplayNameStringRefs * stringRefLen
)

type providerTable uint8

const (
	providerTableLocales providerTable = iota
	providerTableRegions
	providerTableFractions
	providerTableCurrencySymbols
	providerTableBCP47
	providerTableListPatterns
	providerTableUnitPatterns
	providerTableCompactPatterns
	providerTableRelativePatterns
	providerTableRelativeSpecials
	providerTableIntervalPatterns
	providerTableDisplayNames
	providerTableCount
)

func providerTableHeaderOff(table providerTable) int {
	return providerPrefixLen + int(table)*providerTableHeaderLen
}

func providerRowRefSlots(rowLen int) int {
	return rowLen / stringRefLen
}

type providerData struct {
	data []byte
	pool []byte

	localesOff, localesCount     uint32
	regionsOff, regionsCount     uint32
	fractionsOff, fractionsCount uint32
	symbolsOff, symbolsCount     uint32
	bcp47Off, bcp47Count         uint32
	listsOff, listsCount         uint32
	unitsOff, unitsCount         uint32
	compactOff, compactCount     uint32
	relOff, relCount             uint32
	relSpecOff, relSpecCount     uint32
	intervalOff, intervalCount   uint32
	displayOff, displayCount     uint32
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
	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].Locale+"\x00"+symbols[i].Code < symbols[j].Locale+"\x00"+symbols[j].Code
	})
	var symbolRows bytes.Buffer
	for _, rec := range symbols {
		putRef(&symbolRows, pool.ref(rec.Locale))
		putRef(&symbolRows, pool.ref(rec.Code))
		putRef(&symbolRows, pool.ref(rec.Symbol))
		putRef(&symbolRows, pool.ref(rec.Narrow))
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
	lists := provider.AvailableListPatterns()
	sort.Slice(lists, func(i, j int) bool {
		return lists[i].Locale+"\x00"+lists[i].Type+"\x00"+lists[i].Width < lists[j].Locale+"\x00"+lists[j].Type+"\x00"+lists[j].Width
	})
	var listRows bytes.Buffer
	for _, rec := range lists {
		putRef(&listRows, pool.ref(rec.Locale))
		putRef(&listRows, pool.ref(rec.Type))
		putRef(&listRows, pool.ref(rec.Width))
		putRef(&listRows, pool.ref(rec.Pattern.Two))
		putRef(&listRows, pool.ref(rec.Pattern.Start))
		putRef(&listRows, pool.ref(rec.Pattern.Middle))
		putRef(&listRows, pool.ref(rec.Pattern.End))
	}
	units := provider.AvailableUnitPatterns()
	sort.Slice(units, func(i, j int) bool {
		a, b := units[i], units[j]
		return a.Locale+"\x00"+a.Unit+"\x00"+a.Width+"\x00"+a.Category < b.Locale+"\x00"+b.Unit+"\x00"+b.Width+"\x00"+b.Category
	})
	var unitRows bytes.Buffer
	for _, rec := range units {
		putRef(&unitRows, pool.ref(rec.Locale))
		putRef(&unitRows, pool.ref(rec.Unit))
		putRef(&unitRows, pool.ref(rec.Width))
		putRef(&unitRows, pool.ref(rec.Category))
		putRef(&unitRows, pool.ref(rec.Pattern))
	}
	compacts := provider.AvailableCompactPatterns()
	sort.Slice(compacts, func(i, j int) bool {
		a, b := compacts[i], compacts[j]
		return a.Locale+"\x00"+a.Width+"\x00"+fmt.Sprintf("%020d", a.Magnitude)+"\x00"+a.Category <
			b.Locale+"\x00"+b.Width+"\x00"+fmt.Sprintf("%020d", b.Magnitude)+"\x00"+b.Category
	})
	var compactRows bytes.Buffer
	for _, rec := range compacts {
		putRef(&compactRows, pool.ref(rec.Locale))
		putRef(&compactRows, pool.ref(rec.Width))
		putRef(&compactRows, pool.ref(fmt.Sprintf("%020d", rec.Magnitude)))
		putRef(&compactRows, pool.ref(rec.Category))
		putRef(&compactRows, pool.ref(rec.Pattern))
	}
	relatives := provider.AvailableRelativeTimePatterns()
	sort.Slice(relatives, func(i, j int) bool {
		a, b := relatives[i], relatives[j]
		return a.Locale+"\x00"+a.Field+"\x00"+a.Width+"\x00"+a.Direction+"\x00"+a.Category <
			b.Locale+"\x00"+b.Field+"\x00"+b.Width+"\x00"+b.Direction+"\x00"+b.Category
	})
	var relRows bytes.Buffer
	for _, rec := range relatives {
		putRef(&relRows, pool.ref(rec.Locale))
		putRef(&relRows, pool.ref(rec.Field))
		putRef(&relRows, pool.ref(rec.Width))
		putRef(&relRows, pool.ref(rec.Direction))
		putRef(&relRows, pool.ref(rec.Category))
		putRef(&relRows, pool.ref(rec.Pattern))
	}
	specials := provider.AvailableRelativeSpecials()
	sort.Slice(specials, func(i, j int) bool {
		a, b := specials[i], specials[j]
		return a.Locale+"\x00"+a.Field+"\x00"+a.Width+"\x00"+fmt.Sprintf("%020d", a.Offset) <
			b.Locale+"\x00"+b.Field+"\x00"+b.Width+"\x00"+fmt.Sprintf("%020d", b.Offset)
	})
	var specialRows bytes.Buffer
	for _, rec := range specials {
		putRef(&specialRows, pool.ref(rec.Locale))
		putRef(&specialRows, pool.ref(rec.Field))
		putRef(&specialRows, pool.ref(rec.Width))
		putRef(&specialRows, pool.ref(fmt.Sprintf("%020d", rec.Offset)))
		putRef(&specialRows, pool.ref(rec.Text))
	}
	intervals := provider.AvailableIntervalPatterns()
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Locale+"\x00"+intervals[i].Skeleton+"\x00"+intervals[i].Field < intervals[j].Locale+"\x00"+intervals[j].Skeleton+"\x00"+intervals[j].Field
	})
	var intervalRows bytes.Buffer
	for _, rec := range intervals {
		putRef(&intervalRows, pool.ref(rec.Locale))
		putRef(&intervalRows, pool.ref(rec.Skeleton))
		putRef(&intervalRows, pool.ref(rec.Field))
		putRef(&intervalRows, pool.ref(rec.Pattern))
	}
	names := provider.AvailableDisplayNames()
	sort.Slice(names, func(i, j int) bool {
		return names[i].Locale+"\x00"+names[i].Kind+"\x00"+names[i].Code < names[j].Locale+"\x00"+names[j].Kind+"\x00"+names[j].Code
	})
	var nameRows bytes.Buffer
	for _, rec := range names {
		putRef(&nameRows, pool.ref(rec.Locale))
		putRef(&nameRows, pool.ref(rec.Kind))
		putRef(&nameRows, pool.ref(rec.Code))
		putRef(&nameRows, pool.ref(rec.Name))
	}
	stringBytes := pool.bytes()
	total := providerHdrLen + localeRows.Len() + regionRows.Len() + fractionRows.Len() + symbolRows.Len() + bcpRows.Len() +
		listRows.Len() + unitRows.Len() + compactRows.Len() + relRows.Len() + specialRows.Len() + intervalRows.Len() + nameRows.Len() + len(stringBytes)
	out := make([]byte, total)
	copy(out[:4], magicProv)
	binary.LittleEndian.PutUint32(out[4:8], mustU32(providerHdrLen))
	pos := providerHdrLen
	pos = putProviderTable(out, providerTableLocales, pos, localeRows.Bytes(), localeRowLen)
	pos = putProviderTable(out, providerTableRegions, pos, regionRows.Bytes(), regionRowLen)
	pos = putProviderTable(out, providerTableFractions, pos, fractionRows.Bytes(), fractionRowLen)
	pos = putProviderTable(out, providerTableCurrencySymbols, pos, symbolRows.Bytes(), symbolRowLen)
	pos = putProviderTable(out, providerTableBCP47, pos, bcpRows.Bytes(), bcp47RowLen)
	pos = putProviderTable(out, providerTableListPatterns, pos, listRows.Bytes(), listPatternRowLen)
	pos = putProviderTable(out, providerTableUnitPatterns, pos, unitRows.Bytes(), unitPatternRowLen)
	pos = putProviderTable(out, providerTableCompactPatterns, pos, compactRows.Bytes(), compactPatternRowLen)
	pos = putProviderTable(out, providerTableRelativePatterns, pos, relRows.Bytes(), relativePatternRowLen)
	pos = putProviderTable(out, providerTableRelativeSpecials, pos, specialRows.Bytes(), relativeSpecialRowLen)
	pos = putProviderTable(out, providerTableIntervalPatterns, pos, intervalRows.Bytes(), intervalPatternRowLen)
	pos = putProviderTable(out, providerTableDisplayNames, pos, nameRows.Bytes(), displayNameRowLen)
	binary.LittleEndian.PutUint32(out[providerStringPoolOff:providerStringPoolLenOff], mustU32(pos))
	binary.LittleEndian.PutUint32(out[providerStringPoolLenOff:providerHdrLen], mustU32(len(stringBytes)))
	copy(out[pos:], stringBytes)
	records := mustU32(len(locales) + len(regions) + len(fractions) + len(symbols) + len(bcpRecords) + len(lists) +
		len(units) + len(compacts) + len(relatives) + len(specials) + len(intervals) + len(names))
	return out, records, nil
}

func putProviderTable(out []byte, table providerTable, pos int, data []byte, elemSize int) int {
	copy(out[pos:], data)
	hdrOff := providerTableHeaderOff(table)
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
	putStringArray(buf, pool, rec.DayPeriods[:])
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
	if binary.LittleEndian.Uint32(data[4:8]) != mustU32(providerHdrLen) {
		return providerData{}, fmt.Errorf("open cldrpack: unsupported provider header length")
	}
	p := providerData{data: data}
	var err error
	if p.localesOff, p.localesCount, err = providerTableHeader(data, providerTableLocales, localeRowLen); err != nil {
		return providerData{}, err
	}
	if p.regionsOff, p.regionsCount, err = providerTableHeader(data, providerTableRegions, regionRowLen); err != nil {
		return providerData{}, err
	}
	if p.fractionsOff, p.fractionsCount, err = providerTableHeader(data, providerTableFractions, fractionRowLen); err != nil {
		return providerData{}, err
	}
	if p.symbolsOff, p.symbolsCount, err = providerTableHeader(data, providerTableCurrencySymbols, symbolRowLen); err != nil {
		return providerData{}, err
	}
	if p.bcp47Off, p.bcp47Count, err = providerTableHeader(data, providerTableBCP47, bcp47RowLen); err != nil {
		return providerData{}, err
	}
	if p.listsOff, p.listsCount, err = providerTableHeader(data, providerTableListPatterns, listPatternRowLen); err != nil {
		return providerData{}, err
	}
	if p.unitsOff, p.unitsCount, err = providerTableHeader(data, providerTableUnitPatterns, unitPatternRowLen); err != nil {
		return providerData{}, err
	}
	if p.compactOff, p.compactCount, err = providerTableHeader(data, providerTableCompactPatterns, compactPatternRowLen); err != nil {
		return providerData{}, err
	}
	if p.relOff, p.relCount, err = providerTableHeader(data, providerTableRelativePatterns, relativePatternRowLen); err != nil {
		return providerData{}, err
	}
	if p.relSpecOff, p.relSpecCount, err = providerTableHeader(data, providerTableRelativeSpecials, relativeSpecialRowLen); err != nil {
		return providerData{}, err
	}
	if p.intervalOff, p.intervalCount, err = providerTableHeader(data, providerTableIntervalPatterns, intervalPatternRowLen); err != nil {
		return providerData{}, err
	}
	if p.displayOff, p.displayCount, err = providerTableHeader(data, providerTableDisplayNames, displayNameRowLen); err != nil {
		return providerData{}, err
	}
	poolOff := binary.LittleEndian.Uint32(data[providerStringPoolOff:providerStringPoolLenOff])
	poolLen := binary.LittleEndian.Uint32(data[providerStringPoolLenOff:providerHdrLen])
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

func providerTableHeader(data []byte, table providerTable, elemSize int) (uint32, uint32, error) {
	off := providerTableHeaderOff(table)
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
	for i := range rec.DayPeriods {
		rec.DayPeriods[i] = next()
	}
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

func (p providerData) symbol(locale, code string, display cldr.CurrencyDisplayMode) (string, bool) {
	if display == cldr.CurrencyDisplayCode {
		code = strings.ToUpper(strings.TrimSpace(code))
		return code, code != ""
	}
	locale = cldr.CanonicalTag(locale)
	code = strings.ToUpper(strings.TrimSpace(code))
	key := locale + "\x00" + code
	i := sort.Search(int(p.symbolsCount), func(i int) bool {
		return p.symbolKey(i) >= key
	})
	if i < int(p.symbolsCount) && p.symbolKey(i) == key {
		row := p.row(p.symbolsOff, i, symbolRowLen)
		if display == cldr.CurrencyDisplayNarrowSymbol {
			if s := p.ref(row[24:32]); s != "" {
				return s, true
			}
		}
		if s := p.ref(row[16:24]); s != "" {
			return s, true
		}
	}
	return "", false
}

func (p providerData) symbolKey(i int) string {
	row := p.row(p.symbolsOff, i, symbolRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16])
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

func (p providerData) listPattern(locale, typ, width string) (cldr.ListPattern, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + strings.TrimSpace(typ) + "\x00" + normalizePackWidth(width)
	i := sort.Search(int(p.listsCount), func(i int) bool { return p.listKey(i) >= key })
	if i < int(p.listsCount) && p.listKey(i) == key {
		row := p.row(p.listsOff, i, listPatternRowLen)
		return cldr.ListPattern{Two: p.ref(row[24:32]), Start: p.ref(row[32:40]), Middle: p.ref(row[40:48]), End: p.ref(row[48:56])}, true
	}
	return cldr.ListPattern{}, false
}

func (p providerData) listKey(i int) string {
	row := p.row(p.listsOff, i, listPatternRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24])
}

func (p providerData) unitPattern(locale, unit, width, category string) (string, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + strings.TrimSpace(unit) + "\x00" + normalizePackWidth(width) + "\x00" + normalizePackCategory(category)
	i := sort.Search(int(p.unitsCount), func(i int) bool { return p.unitKey(i) >= key })
	if i < int(p.unitsCount) && p.unitKey(i) == key {
		return p.ref(p.row(p.unitsOff, i, unitPatternRowLen)[32:40]), true
	}
	if category != "other" {
		return p.unitPattern(locale, unit, width, "other")
	}
	return "", false
}

func (p providerData) unitKey(i int) string {
	row := p.row(p.unitsOff, i, unitPatternRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24]) + "\x00" + p.ref(row[24:32])
}

func (p providerData) compactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + normalizePackCompactWidth(width) + "\x00" + fmt.Sprintf("%020d", magnitude) + "\x00" + normalizePackCategory(category)
	i := sort.Search(int(p.compactCount), func(i int) bool { return p.compactKey(i) >= key })
	if i < int(p.compactCount) && p.compactKey(i) == key {
		return p.ref(p.row(p.compactOff, i, compactPatternRowLen)[32:40]), true
	}
	if category != "other" {
		return p.compactPattern(locale, width, magnitude, "other")
	}
	return "", false
}

func (p providerData) compactKey(i int) string {
	row := p.row(p.compactOff, i, compactPatternRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24]) + "\x00" + p.ref(row[24:32])
}

func (p providerData) relativePattern(locale, field, width, direction, category string) (string, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + strings.TrimSpace(field) + "\x00" + normalizePackWidth(width) + "\x00" + strings.TrimSpace(direction) + "\x00" + normalizePackCategory(category)
	i := sort.Search(int(p.relCount), func(i int) bool { return p.relativeKey(i) >= key })
	if i < int(p.relCount) && p.relativeKey(i) == key {
		return p.ref(p.row(p.relOff, i, relativePatternRowLen)[40:48]), true
	}
	if category != "other" {
		return p.relativePattern(locale, field, width, direction, "other")
	}
	return "", false
}

func (p providerData) relativeKey(i int) string {
	row := p.row(p.relOff, i, relativePatternRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24]) + "\x00" + p.ref(row[24:32]) + "\x00" + p.ref(row[32:40])
}

func (p providerData) relativeSpecial(locale, field, width string, offset int) (string, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + strings.TrimSpace(field) + "\x00" + normalizePackWidth(width) + "\x00" + fmt.Sprintf("%020d", offset)
	i := sort.Search(int(p.relSpecCount), func(i int) bool { return p.relativeSpecialKey(i) >= key })
	if i < int(p.relSpecCount) && p.relativeSpecialKey(i) == key {
		return p.ref(p.row(p.relSpecOff, i, relativeSpecialRowLen)[32:40]), true
	}
	return "", false
}

func (p providerData) relativeSpecialKey(i int) string {
	row := p.row(p.relSpecOff, i, relativeSpecialRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24]) + "\x00" + p.ref(row[24:32])
}

func (p providerData) intervalPattern(locale, skeleton, field string) (string, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + strings.TrimSpace(skeleton) + "\x00" + strings.TrimSpace(field)
	i := sort.Search(int(p.intervalCount), func(i int) bool { return p.intervalKey(i) >= key })
	if i < int(p.intervalCount) && p.intervalKey(i) == key {
		return p.ref(p.row(p.intervalOff, i, intervalPatternRowLen)[24:32]), true
	}
	return "", false
}

func (p providerData) intervalKey(i int) string {
	row := p.row(p.intervalOff, i, intervalPatternRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24])
}

func (p providerData) displayName(locale, kind, code string) (string, bool) {
	key := cldr.CanonicalTag(locale) + "\x00" + strings.TrimSpace(kind) + "\x00" + strings.TrimSpace(code)
	i := sort.Search(int(p.displayCount), func(i int) bool { return p.displayKey(i) >= key })
	if i < int(p.displayCount) && p.displayKey(i) == key {
		return p.ref(p.row(p.displayOff, i, displayNameRowLen)[24:32]), true
	}
	return "", false
}

func (p providerData) displayKey(i int) string {
	row := p.row(p.displayOff, i, displayNameRowLen)
	return p.ref(row[0:8]) + "\x00" + p.ref(row[8:16]) + "\x00" + p.ref(row[16:24])
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
		out[i] = cldr.CurrencySymbolRecord{Locale: p.ref(row[0:8]), Code: p.ref(row[8:16]), Symbol: p.ref(row[16:24]), Narrow: p.ref(row[24:32])}
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

func (p providerData) listPatterns() []cldr.ListPatternRecord {
	out := make([]cldr.ListPatternRecord, int(p.listsCount))
	for i := range out {
		row := p.row(p.listsOff, i, listPatternRowLen)
		out[i] = cldr.ListPatternRecord{
			Locale: p.ref(row[0:8]),
			Type:   p.ref(row[8:16]),
			Width:  p.ref(row[16:24]),
			Pattern: cldr.ListPattern{
				Two:    p.ref(row[24:32]),
				Start:  p.ref(row[32:40]),
				Middle: p.ref(row[40:48]),
				End:    p.ref(row[48:56]),
			},
		}
	}
	return out
}

func (p providerData) unitPatterns() []cldr.UnitPatternRecord {
	out := make([]cldr.UnitPatternRecord, int(p.unitsCount))
	for i := range out {
		row := p.row(p.unitsOff, i, unitPatternRowLen)
		out[i] = cldr.UnitPatternRecord{
			Locale:   p.ref(row[0:8]),
			Unit:     p.ref(row[8:16]),
			Width:    p.ref(row[16:24]),
			Category: p.ref(row[24:32]),
			Pattern:  p.ref(row[32:40]),
		}
	}
	return out
}

func (p providerData) compactPatterns() []cldr.CompactPatternRecord {
	out := make([]cldr.CompactPatternRecord, int(p.compactCount))
	for i := range out {
		row := p.row(p.compactOff, i, compactPatternRowLen)
		out[i] = cldr.CompactPatternRecord{
			Locale:    p.ref(row[0:8]),
			Width:     p.ref(row[8:16]),
			Magnitude: parsePackInt64(p.ref(row[16:24])),
			Category:  p.ref(row[24:32]),
			Pattern:   p.ref(row[32:40]),
		}
	}
	return out
}

func (p providerData) relativePatterns() []cldr.RelativePatternRecord {
	out := make([]cldr.RelativePatternRecord, int(p.relCount))
	for i := range out {
		row := p.row(p.relOff, i, relativePatternRowLen)
		out[i] = cldr.RelativePatternRecord{
			Locale:    p.ref(row[0:8]),
			Field:     p.ref(row[8:16]),
			Width:     p.ref(row[16:24]),
			Direction: p.ref(row[24:32]),
			Category:  p.ref(row[32:40]),
			Pattern:   p.ref(row[40:48]),
		}
	}
	return out
}

func (p providerData) relativeSpecials() []cldr.RelativeSpecialRecord {
	out := make([]cldr.RelativeSpecialRecord, int(p.relSpecCount))
	for i := range out {
		row := p.row(p.relSpecOff, i, relativeSpecialRowLen)
		out[i] = cldr.RelativeSpecialRecord{
			Locale: p.ref(row[0:8]),
			Field:  p.ref(row[8:16]),
			Width:  p.ref(row[16:24]),
			Offset: parsePackInt(p.ref(row[24:32])),
			Text:   p.ref(row[32:40]),
		}
	}
	return out
}

func (p providerData) intervalPatterns() []cldr.IntervalPatternRecord {
	out := make([]cldr.IntervalPatternRecord, int(p.intervalCount))
	for i := range out {
		row := p.row(p.intervalOff, i, intervalPatternRowLen)
		out[i] = cldr.IntervalPatternRecord{
			Locale:   p.ref(row[0:8]),
			Skeleton: p.ref(row[8:16]),
			Field:    p.ref(row[16:24]),
			Pattern:  p.ref(row[24:32]),
		}
	}
	return out
}

func (p providerData) displayNames() []cldr.DisplayNameRecord {
	out := make([]cldr.DisplayNameRecord, int(p.displayCount))
	for i := range out {
		row := p.row(p.displayOff, i, displayNameRowLen)
		out[i] = cldr.DisplayNameRecord{
			Locale: p.ref(row[0:8]),
			Kind:   p.ref(row[8:16]),
			Code:   p.ref(row[16:24]),
			Name:   p.ref(row[24:32]),
		}
	}
	return out
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
		{name: "locale", off: p.localesOff, count: p.localesCount, rowLen: localeRowLen, refSlots: providerRowRefSlots(localeRowLen)},
		{name: "region", off: p.regionsOff, count: p.regionsCount, rowLen: regionRowLen, refSlots: providerRowRefSlots(regionRowLen)},
		{name: "fraction", off: p.fractionsOff, count: p.fractionsCount, rowLen: fractionRowLen, refSlots: providerFractionStringRefs},
		{name: "symbol", off: p.symbolsOff, count: p.symbolsCount, rowLen: symbolRowLen, refSlots: providerRowRefSlots(symbolRowLen)},
		{name: "bcp47", off: p.bcp47Off, count: p.bcp47Count, rowLen: bcp47RowLen, refSlots: providerRowRefSlots(bcp47RowLen)},
		{name: "list pattern", off: p.listsOff, count: p.listsCount, rowLen: listPatternRowLen, refSlots: providerRowRefSlots(listPatternRowLen)},
		{name: "unit pattern", off: p.unitsOff, count: p.unitsCount, rowLen: unitPatternRowLen, refSlots: providerRowRefSlots(unitPatternRowLen)},
		{name: "compact pattern", off: p.compactOff, count: p.compactCount, rowLen: compactPatternRowLen, refSlots: providerRowRefSlots(compactPatternRowLen)},
		{name: "relative pattern", off: p.relOff, count: p.relCount, rowLen: relativePatternRowLen, refSlots: providerRowRefSlots(relativePatternRowLen)},
		{name: "relative special", off: p.relSpecOff, count: p.relSpecCount, rowLen: relativeSpecialRowLen, refSlots: providerRowRefSlots(relativeSpecialRowLen)},
		{name: "interval pattern", off: p.intervalOff, count: p.intervalCount, rowLen: intervalPatternRowLen, refSlots: providerRowRefSlots(intervalPatternRowLen)},
		{name: "display name", off: p.displayOff, count: p.displayCount, rowLen: displayNameRowLen, refSlots: providerRowRefSlots(displayNameRowLen)},
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

func normalizePackWidth(width string) string {
	switch strings.ToLower(strings.TrimSpace(width)) {
	case "short":
		return "short"
	case "narrow":
		return "narrow"
	default:
		return "long"
	}
}

func normalizePackCompactWidth(width string) string {
	if strings.EqualFold(strings.TrimSpace(width), "long") {
		return "long"
	}
	return "short"
}

func normalizePackCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "zero", "one", "two", "few", "many":
		return strings.ToLower(strings.TrimSpace(category))
	default:
		return "other"
	}
}

func parsePackInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func parsePackInt64(value string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n
}
