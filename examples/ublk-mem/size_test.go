package main

import "testing"

func TestParseSize_PlainDigits(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"512", 512},
		{"0", 0},
		{"1024", 1024},
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

func TestParseSize_RejectsNonNumeric(t *testing.T) {
	for _, in := range []string{"abc", ""} {
		if got, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = %d, nil; want non-nil error", in, got)
		}
	}
}

func TestParseSize_Suffixes(t *testing.T) {
	// Hand-derived: "1K" = 1*1024 = 1024; "64M" = 64*1024*1024 = 67108864;
	// "1G" = 1*1024*1024*1024 = 1073741824.
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

func TestParseSize_CaseInsensitiveSuffix(t *testing.T) {
	// strings.ToUpper(s) runs before the suffix checks, so lowercase and
	// mixed-case suffixes must parse identically to their uppercase forms.
	pairs := []struct{ lower, upper string }{
		{"64m", "64M"},
		{"1g", "1G"},
		{"512k", "512K"},
	}
	for _, p := range pairs {
		lower, errL := parseSize(p.lower)
		upper, errU := parseSize(p.upper)
		if errL != nil || errU != nil {
			t.Errorf("parseSize(%q)(%v) / parseSize(%q)(%v): unexpected error", p.lower, errL, p.upper, errU)
			continue
		}
		if lower != upper {
			t.Errorf("parseSize(%q) = %d, want %d (== parseSize(%q))", p.lower, lower, upper, p.upper)
		}
	}
}

func TestParseSize_SuffixAlone(t *testing.T) {
	// parseSize("M") -> ToUpper -> HasSuffix("M") true, so numStr = "".
	// strconv.ParseInt("", 10, 64) errors on an empty string, so the pinned
	// result is (0, error), NOT 0 bytes. Same for "K" and "G".
	for _, in := range []string{"M", "K", "G"} {
		if got, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = %d, nil; want non-nil error (suffix with no digits)", in, got)
		}
	}
}

func TestParseSize_Negative(t *testing.T) {
	// SUSPECTED DEFECT (pinned, not fixed): parseSize never inspects the sign
	// of the parsed number. "-100" and "-1G" both parse successfully and return
	// negative byte counts with a nil error (e.g. "-1G" = -1073741824). The
	// caller in main() passes this straight into newMemoryBackend, where
	// make([]byte, negative) would panic rather than produce a clean error.
	cases := []struct {
		in   string
		want int64
	}{
		{"-100", -100},
		{"-1G", -1073741824},
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

func TestParseSize_Overflow(t *testing.T) {
	// SUSPECTED DEFECT (pinned, not fixed): num * multiplier wraps silently on
	// signed int64 overflow. parseSize("10000000000G") = 10^10 * 2^30 =
	// 10737418240000000000, which exceeds max int64 (2^63-1 = 9223372036854775807)
	// and wraps to -7709325833709551616 with a nil error -- a negative,
	// nonsensical size that is indistinguishable from a legitimate value at the
	// call site. A later -1G-style panic in newMemoryBackend is the result.
	got, err := parseSize("10000000000G")
	if err != nil {
		t.Fatalf("parseSize(\"10000000000G\") unexpected error: %v", err)
	}
	const want int64 = -7709325833709551616
	if got != want {
		t.Errorf("parseSize(\"10000000000G\") = %d, want wrapped %d", got, want)
	}
}

func TestFormatSize_Pairs(t *testing.T) {
	// Hand-derived: 0 -> "0 B", 1023 -> "1023 B", 1024 -> 1024/1024 = "1.0 KB",
	// 1536 -> 1536/1024 = "1.5 KB", 1048576 = 1024^2 -> "1.0 MB",
	// 1099511627776 = 1024^4 = 2^40 -> "1.0 TB".
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1099511627776, "1.0 TB"},
	}
	for _, c := range cases {
		if got := formatSize(c.in); got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatSize_Negative(t *testing.T) {
	// formatSize(-5): bytes < unit (1024) holds for any negative value, so it
	// falls into the "%d B" branch. This is a value that should never
	// legitimately occur, but the function does not guard against it.
	if got := formatSize(-5); got != "-5 B" {
		t.Errorf("formatSize(-5) = %q, want %q", got, "-5 B")
	}
}
