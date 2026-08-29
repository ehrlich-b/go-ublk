package main

import "testing"

func TestParseSizePlainDigits(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"512", 512},
		{"0", 0},
		{"2048", 2048},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSizeRejectsNonNumeric(t *testing.T) {
	for _, in := range []string{"abc", "", "12x34", " 512"} {
		if _, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = nil error, want non-nil", in)
		}
	}
}

func TestParseSizeSuffixes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"1K", 1024},
		{"64M", 67108864},
		{"1G", 1073741824},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSizeLowercaseSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"64m", 67108864},
		{"1g", 1073741824},
		{"512k", 524288},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSizeSuffixAlone(t *testing.T) {
	for _, in := range []string{"K", "M", "G", "k", "m", "g"} {
		if _, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = nil error, want non-nil (empty number string)", in)
		}
	}
}

func TestParseSizeNegative(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"-100", -100},
		{"-1G", -1073741824},
		{"-512K", -524288},
	}
	for _, c := range cases {
		got, err := parseSize(c.in)
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		// SUSPECTED DEFECT (pinned, not fixed): parseSize accepts a leading
		// '-' (strconv.ParseInt allows it) and returns a negative byte count
		// with no error. A device size must never be negative, but nothing in
		// parseSize guards the sign.
		if got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSizeOverflow(t *testing.T) {
	// SUSPECTED DEFECT (pinned, not fixed): strconv.ParseInt bounds the number
	// to int64, but the subsequent multiplication by the G multiplier
	// (1024*1024*1024) is not overflow-checked. int64 multiplication silently
	// wraps on overflow.
	//
	// 8589934592 = 2^33 (fits int64), G multiplier = 2^30, so the product is
	// 2^63 which wraps around to math.MinInt64 with no error.
	got, err := parseSize("8589934592G")
	if err != nil {
		t.Fatalf("parseSize(\"8589934592G\") unexpected error: %v", err)
	}
	const want = int64(-9223372036854775808)
	if got != want {
		t.Errorf("parseSize(\"8589934592G\") = %d, want %d (overflow wraparound)", got, want)
	}
}

func TestFormatSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1099511627776, "1.0 TB"}, // 1024^4
	}
	for _, c := range cases {
		if got := formatSize(c.in); got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatSizeNegative(t *testing.T) {
	// Observation (not a claim it needs fixing): any negative bytes < unit
	// (1024), so formatSize falls into the "%d B" branch and prints the raw
	// negative with no decimal, e.g. "-5 B". A size should never be negative.
	for _, c := range []struct {
		in   int64
		want string
	}{
		{-5, "-5 B"},
		{-1, "-1 B"},
	} {
		if got := formatSize(c.in); got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
