package pdf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceDateEpoch(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		wantOK bool
		want   time.Time
	}{
		{name: "unset", value: "", wantOK: false},
		{name: "valid", value: "1700000000", wantOK: true, want: time.Unix(1700000000, 0).UTC()},
		{name: "invalid", value: "not-a-number", wantOK: false},
		{name: "whitespace", value: "  1700000000  ", wantOK: true, want: time.Unix(1700000000, 0).UTC()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", tt.value)
			got, ok := SourceDateEpoch()
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && !got.Equal(tt.want) {
				t.Fatalf("ts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeDates(t *testing.T) {
	ts := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "same length",
			in:   `<< /CreationDate (D:20260610154212+02'00') /ModDate (D:20260610154213+02'00') >>`,
			want: `<< /CreationDate (D:20231114221320Z00'00') /ModDate (D:20231114221320Z00'00') >>`,
		},
		{
			name: "shorter original is prefix-truncated",
			in:   `/CreationDate (D:20260610)`,
			want: `/CreationDate (D:20231114)`,
		},
		{
			name: "longer original is space-padded",
			in:   `/ModDate (D:20260610154212+02'00'xxxx)`,
			want: `/ModDate (D:20231114221320Z00'00'    )`,
		},
		{
			name: "no dates is a no-op",
			in:   `<< /Producer (Skia/PDF) >>`,
			want: `<< /Producer (Skia/PDF) >>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDates([]byte(tt.in), ts)
			if string(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if len(got) != len(tt.in) {
				t.Fatalf("length changed: %d -> %d", len(tt.in), len(got))
			}
		})
	}
}

func TestNormalizeID(t *testing.T) {
	in := []byte(`trailer << /Size 9 /ID [<A1B2C3D4E5F60718> <293A4B5C6D7E8F90>] >>`)
	got := normalizeID(in)

	if len(got) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(got))
	}
	if bytes.Equal(got, in) {
		t.Fatal("ID was not rewritten")
	}
	// Deterministic: same input always yields the same output.
	if again := normalizeID(in); !bytes.Equal(got, again) {
		t.Fatal("normalizeID is not deterministic")
	}
}

func TestNormalizeForReproducibility(t *testing.T) {
	ts := time.Unix(1700000000, 0).UTC()
	content := `%PDF-1.4
1 0 obj << /CreationDate (D:20260610154212+02'00') /ModDate (D:20260611093015+02'00') >> endobj
trailer << /ID [<0123456789ABCDEF0123456789ABCDEF> <FEDCBA9876543210FEDCBA9876543210>] >>
%%EOF`

	write := func(t *testing.T, name, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return path
	}

	pathA := write(t, "a.pdf", content)
	pathB := write(t, "b.pdf", content)

	// Same content but different original timestamps/IDs must converge.
	divergent := `%PDF-1.4
1 0 obj << /CreationDate (D:20250101000000+00'00') /ModDate (D:20250102000000+00'00') >> endobj
trailer << /ID [<AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA> <BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB>] >>
%%EOF`
	pathC := write(t, "c.pdf", divergent)

	for _, p := range []string{pathA, pathB, pathC} {
		if err := NormalizeForReproducibility(p, ts); err != nil {
			t.Fatalf("normalize %s: %v", p, err)
		}
	}

	dataA, _ := os.ReadFile(pathA)
	dataB, _ := os.ReadFile(pathB)
	dataC, _ := os.ReadFile(pathC)

	if !bytes.Equal(dataA, dataB) {
		t.Fatal("identical inputs did not normalize to identical bytes")
	}
	if !bytes.Equal(dataA, dataC) {
		t.Fatalf("same-length inputs with different metadata did not converge:\nA: %s\nC: %s", dataA, dataC)
	}
	if len(dataA) != len(content) {
		t.Fatalf("file length changed: %d -> %d", len(content), len(dataA))
	}
	if bytes.Contains(dataA, []byte("D:2026")) {
		t.Fatal("original timestamp survived normalization")
	}
}

func TestNormalizeForReproducibility_MissingFile(t *testing.T) {
	err := NormalizeForReproducibility(filepath.Join(t.TempDir(), "missing.pdf"), time.Unix(0, 0))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
