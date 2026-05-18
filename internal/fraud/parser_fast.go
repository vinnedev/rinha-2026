package fraud

// Positional JSON parser tied to the rinha-2026 fraud-score payload.
//
// The schema is fixed and the engine always emits the keys in the same
// order, so we walk bytes from left to right, locate each `:` separator,
// and read the value with a type-specific scanner. Strings are kept as
// byte slices (no allocation), numbers go straight to uint64 "milli"
// integers, and the unknown-merchant comparison happens during the walk.
//
// This replaces fastjson.Unmarshal on the hot path:
//   - no DOM allocation
//   - no string→time.Time parsing
//   - no string allocations for known_merchants
//   - integer-milli output ready for vectorize_int.go
//
// The parser tolerates whitespace between tokens; it does not validate
// every JSON detail. Malformed input returns ok=false and the caller
// answers with the safe response (per AVALIACAO scoring trade-offs).

const knownMerchantsCap = 32

// ParseFast decodes a fraud-score request body into out. Returns false on
// malformed input or schema deviation. zero-alloc on success.
func ParseFast(body []byte, out *IntPayload) bool {
	*out = IntPayload{}
	p := 0
	n := len(body)
	if n < 16 {
		return false
	}

	// "id": "..."   — we don't store, but the value must consume position.
	if !skipKey(body, &p, n) || !skipString(body, &p, n) {
		return false
	}

	// "transaction": { "amount": <num>, "installments": <int>, "requested_at": "<iso>" }
	if !skipKey(body, &p, n) || !skipUntil(body, &p, n, '{') {
		return false
	}
	if !skipKey(body, &p, n) {
		return false
	}
	out.AmountMilli = scanScaled1000(body, &p, n)
	if !skipKey(body, &p, n) {
		return false
	}
	out.Installments = uint8(scanUint(body, &p, n))

	if !skipKey(body, &p, n) {
		return false
	}
	reqY, reqMo, reqD, reqH, reqMin, ok := scanISO(body, &p, n)
	if !ok {
		return false
	}
	out.Hour = reqH
	out.DayOfWeek = dayOfWeek(reqY, reqMo, reqD)
	if !skipUntil(body, &p, n, '}') {
		return false
	}

	// "customer": { "avg_amount": <num>, "tx_count_24h": <int>, "known_merchants": [<strings>] }
	if !skipKey(body, &p, n) || !skipUntil(body, &p, n, '{') {
		return false
	}
	if !skipKey(body, &p, n) {
		return false
	}
	out.CustomerAvgAmountMilli = scanScaled1000(body, &p, n)
	if !skipKey(body, &p, n) {
		return false
	}
	out.TxCount24h = scanUint(body, &p, n)
	if !skipKey(body, &p, n) || !skipUntil(body, &p, n, '[') {
		return false
	}

	var (
		merchants    [knownMerchantsCap][2]int
		merchantsN   int
	)
	for p < n && body[p] != ']' {
		if body[p] == '"' {
			p++
			start := p
			for p < n && body[p] != '"' {
				p++
			}
			if p >= n {
				return false
			}
			if merchantsN < knownMerchantsCap {
				merchants[merchantsN][0] = start
				merchants[merchantsN][1] = p
				merchantsN++
			}
			p++
			continue
		}
		p++
	}
	if p < n {
		p++
	}
	if !skipUntil(body, &p, n, '}') {
		return false
	}

	// "merchant": { "id": "<id>", "mcc": "<mcc>", "avg_amount": <num> }
	if !skipKey(body, &p, n) || !skipUntil(body, &p, n, '{') {
		return false
	}
	if !skipKey(body, &p, n) {
		return false
	}
	idStart, idEnd, ok := scanStringSlice(body, &p, n)
	if !ok {
		return false
	}
	if !skipKey(body, &p, n) {
		return false
	}
	out.Mcc = scanMcc(body, &p, n)
	if !skipKey(body, &p, n) {
		return false
	}
	out.MerchantAvgAmountMilli = scanScaled1000(body, &p, n)
	if !skipUntil(body, &p, n, '}') {
		return false
	}

	// "terminal": { "is_online": <bool>, "card_present": <bool>, "km_from_home": <num> }
	if !skipKey(body, &p, n) || !skipUntil(body, &p, n, '{') {
		return false
	}
	if !skipKey(body, &p, n) {
		return false
	}
	out.IsOnline = scanBool(body, &p, n)
	if !skipKey(body, &p, n) {
		return false
	}
	out.CardPresent = scanBool(body, &p, n)
	if !skipKey(body, &p, n) {
		return false
	}
	out.KmFromHomeMilli = scanScaled1000(body, &p, n)
	if !skipUntil(body, &p, n, '}') {
		return false
	}

	// "last_transaction": null | { "timestamp": "<iso>", "km_from_current": <num> }
	if !skipKey(body, &p, n) {
		return false
	}
	skipWhitespace(body, &p, n)
	if p >= n {
		return false
	}
	if body[p] == 'n' {
		// null — skip 4 chars
		p += 4
	} else if body[p] == '{' {
		p++
		if !skipKey(body, &p, n) {
			return false
		}
		ltY, ltMo, ltD, ltH, ltMin, ok := scanISO(body, &p, n)
		if !ok {
			return false
		}
		if !skipKey(body, &p, n) {
			return false
		}
		out.KmFromCurrentMilli = scanScaled1000(body, &p, n)
		out.HasLastTx = true
		out.MinutesSinceLast = minutesBetween(
			ltY, ltMo, ltD, ltH, ltMin,
			reqY, reqMo, reqD, reqH, reqMin,
		)
	}

	out.IsUnknownMerchant = true
	idLen := idEnd - idStart
	for i := 0; i < merchantsN; i++ {
		mLen := merchants[i][1] - merchants[i][0]
		if mLen != idLen {
			continue
		}
		match := true
		for j := 0; j < idLen; j++ {
			if body[merchants[i][0]+j] != body[idStart+j] {
				match = false
				break
			}
		}
		if match {
			out.IsUnknownMerchant = false
			break
		}
	}

	return true
}

func skipKey(body []byte, p *int, n int) bool {
	for *p < n {
		c := body[*p]
		switch c {
		case ':':
			*p++
			skipWhitespace(body, p, n)
			return true
		case '"':
			*p++
			for *p < n && body[*p] != '"' {
				*p++
			}
			if *p >= n {
				return false
			}
			*p++
		default:
			*p++
		}
	}
	return false
}

func skipUntil(body []byte, p *int, n int, target byte) bool {
	for *p < n {
		c := body[*p]
		if c == target {
			*p++
			skipWhitespace(body, p, n)
			return true
		}
		*p++
	}
	return false
}

func skipString(body []byte, p *int, n int) bool {
	if *p < n && body[*p] == '"' {
		*p++
	}
	for *p < n && body[*p] != '"' {
		*p++
	}
	if *p >= n {
		return false
	}
	*p++
	return true
}

func skipWhitespace(body []byte, p *int, n int) {
	for *p < n {
		switch body[*p] {
		case ' ', '\t', '\n', '\r':
			*p++
		default:
			return
		}
	}
}

// scanScaled1000 reads a decimal number into integer "milli" units. It
// matches the convention from the C top-1 parse_scaled1000: round-half-up,
// up to 3 fractional digits considered (with rounding from the 4th), and
// negatives clamp to 0.
func scanScaled1000(body []byte, p *int, n int) uint64 {
	if *p >= n {
		return 0
	}
	negative := false
	if body[*p] == '-' {
		negative = true
		*p++
	}
	var intPart uint64
	for *p < n {
		c := body[*p]
		if c < '0' || c > '9' {
			break
		}
		intPart = intPart*10 + uint64(c-'0')
		*p++
	}
	value := intPart * 1000
	if *p < n && body[*p] == '.' {
		*p++
		var frac uint32
		digits := 0
		roundDigit := -1
		for *p < n {
			c := body[*p]
			if c < '0' || c > '9' {
				break
			}
			if digits < 3 {
				frac = frac*10 + uint32(c-'0')
			} else if digits == 3 {
				roundDigit = int(c - '0')
			}
			digits++
			*p++
		}
		for digits < 3 {
			frac *= 10
			digits++
		}
		value += uint64(frac)
		if roundDigit >= 5 {
			value++
		}
	}
	if *p < n && (body[*p] == 'e' || body[*p] == 'E') {
		*p++
		sign := 1
		if *p < n {
			if body[*p] == '-' {
				sign = -1
				*p++
			} else if body[*p] == '+' {
				*p++
			}
		}
		var exp int
		for *p < n {
			c := body[*p]
			if c < '0' || c > '9' {
				break
			}
			exp = exp*10 + int(c-'0')
			*p++
		}
		if sign > 0 {
			for exp > 0 {
				value *= 10
				exp--
			}
		} else {
			for exp > 0 {
				value = (value + 5) / 10
				exp--
			}
		}
	}
	if negative {
		return 0
	}
	return value
}

func scanUint(body []byte, p *int, n int) uint32 {
	var v uint32
	for *p < n {
		c := body[*p]
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + uint32(c-'0')
		*p++
	}
	return v
}

func scanBool(body []byte, p *int, n int) bool {
	if *p >= n {
		return false
	}
	if body[*p] == 't' {
		*p += 4
		return true
	}
	*p += 5
	return false
}

// scanStringSlice records the byte positions of a JSON string content
// (between quotes) without allocating.
func scanStringSlice(body []byte, p *int, n int) (start, end int, ok bool) {
	if *p >= n || body[*p] != '"' {
		return 0, 0, false
	}
	*p++
	start = *p
	for *p < n && body[*p] != '"' {
		*p++
	}
	if *p >= n {
		return 0, 0, false
	}
	end = *p
	*p++
	return start, end, true
}

// scanMcc accepts either `"5411"` or `5411` (with quotes optional).
func scanMcc(body []byte, p *int, n int) uint32 {
	if *p < n && body[*p] == '"' {
		*p++
	}
	v := scanUint(body, p, n)
	if *p < n && body[*p] == '"' {
		*p++
	}
	return v
}

// scanISO parses YYYY-MM-DDTHH:MM:SS(...). Skips through closing quote so
// any timezone suffix ("Z", "+00:00", ...) is consumed.
func scanISO(body []byte, p *int, n int) (year uint16, month, day, hour, minute uint8, ok bool) {
	if *p < n && body[*p] == '"' {
		*p++
	}
	if n-*p < 16 {
		return 0, 0, 0, 0, 0, false
	}
	s := body[*p : *p+16]
	year = uint16(int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0'))
	month = (s[5]-'0')*10 + (s[6] - '0')
	day = (s[8]-'0')*10 + (s[9] - '0')
	hour = (s[11]-'0')*10 + (s[12] - '0')
	minute = (s[14]-'0')*10 + (s[15] - '0')
	*p += 16
	for *p < n && body[*p] != '"' {
		*p++
	}
	if *p < n {
		*p++
	}
	return year, month, day, hour, minute, true
}

// dayOfWeek returns Mon=0..Sun=6 (Zeller-style adjusted).
func dayOfWeek(year uint16, month, day uint8) uint8 {
	var t = [12]uint16{0, 3, 2, 5, 0, 3, 5, 1, 4, 6, 2, 4}
	y := uint32(year)
	if month < 3 {
		y--
	}
	dow := (y + y/4 - y/100 + y/400 + uint32(t[month-1]) + uint32(day)) % 7
	return uint8((dow + 6) % 7)
}

// daysSinceEpoch implements the civil_from_days inverse — number of days
// from the proleptic Gregorian calendar epoch 1970-01-01 to (year, month,
// day). Mirrors the C top-1 implementation exactly so the minute-diff
// math is identical.
func daysSinceEpoch(year int32, month, day uint32) int64 {
	y := year
	if month <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yoe := uint32(y - era*400)
	m := month
	if m > 2 {
		m -= 3
	} else {
		m += 9
	}
	doy := (153*m+2)/5 + day - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return int64(era)*146097 + int64(doe) - 719468
}

func minutesBetween(y1 uint16, mo1, d1, h1, mi1 uint8, y2 uint16, mo2, d2, h2, mi2 uint8) uint32 {
	day1 := daysSinceEpoch(int32(y1), uint32(mo1), uint32(d1))
	day2 := daysSinceEpoch(int32(y2), uint32(mo2), uint32(d2))
	min1 := day1*1440 + int64(h1)*60 + int64(mi1)
	min2 := day2*1440 + int64(h2)*60 + int64(mi2)
	if min2 <= min1 {
		return 0
	}
	return uint32(min2 - min1)
}
