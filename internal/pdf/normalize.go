package pdf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SourceDateEpoch returns the timestamp from the SOURCE_DATE_EPOCH environment
// variable (reproducible-builds.org convention). The second return value is
// false when the variable is unset or not a valid integer.
func SourceDateEpoch() (time.Time, bool) {
	raw := strings.TrimSpace(os.Getenv("SOURCE_DATE_EPOCH"))
	if raw == "" {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(secs, 0).UTC(), true
}

var (
	pdfDateRe = regexp.MustCompile(`(/CreationDate|/ModDate)\s*\(D:[^)]*\)`)
	pdfIDRe   = regexp.MustCompile(`/ID\s*\[\s*<([0-9a-fA-F]+)>\s*<([0-9a-fA-F]+)>\s*\]`)
)

// NormalizeForReproducibility rewrites the PDF's CreationDate/ModDate entries
// and its document ID in place so that builds of identical content produce
// byte-identical files. Dates are set to ts; the document ID is derived from
// a hash of the (date-normalized) file content. All replacements preserve the
// original byte length, keeping cross-reference offsets valid.
func NormalizeForReproducibility(path string, ts time.Time) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("normalize pdf: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("normalize pdf: %w", err)
	}

	data = normalizeDates(data, ts)
	data = normalizeID(data)

	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil { //nolint:gosec // G703: rewrites the pipeline-owned PDF that was just read above
		return fmt.Errorf("normalize pdf: %w", err)
	}
	return nil
}

// normalizeDates replaces every /CreationDate and /ModDate string with ts,
// padded or prefix-truncated to the original length. Any prefix of a PDF date
// string (down to D:YYYY) is itself a valid, less precise date, so truncation
// never corrupts the entry.
func normalizeDates(data []byte, ts time.Time) []byte {
	canonical := "D:" + ts.UTC().Format("20060102150405") + "Z00'00'"
	return pdfDateRe.ReplaceAllFunc(data, func(match []byte) []byte {
		open := strings.IndexByte(string(match), '(')
		// Space inside the parentheses available for the date string.
		room := len(match) - open - 2
		date := canonical
		if room < len(date) {
			date = date[:room]
		} else if room > len(date) {
			date += strings.Repeat(" ", room-len(date))
		}
		return append(match[:open+1], append([]byte(date), ')')...)
	})
}

// normalizeID replaces both hex strings of every trailer /ID entry with a
// digest of the file content, repeated or truncated to the original length.
// The digest is computed with the old ID bytes masked out, so two files that
// differ only in their (random) IDs converge to the same bytes.
func normalizeID(data []byte) []byte {
	locs := pdfIDRe.FindAllSubmatchIndex(data, -1)
	if len(locs) == 0 {
		return data
	}

	out := make([]byte, len(data))
	copy(out, data)
	for _, loc := range locs {
		for _, span := range [][2]int{{loc[2], loc[3]}, {loc[4], loc[5]}} {
			for i := span[0]; i < span[1]; i++ {
				out[i] = '0'
			}
		}
	}

	digest := sha256.Sum256(out)
	hexDigest := hex.EncodeToString(digest[:])
	for _, loc := range locs {
		for _, span := range [][2]int{{loc[2], loc[3]}, {loc[4], loc[5]}} {
			copy(out[span[0]:span[1]], fitHex(hexDigest, span[1]-span[0]))
		}
	}
	return out
}

// fitHex repeats or truncates src to exactly n characters.
func fitHex(src string, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = src[i%len(src)]
	}
	return out
}
