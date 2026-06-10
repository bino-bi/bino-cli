package httpserver

import "testing"

func TestDataStorePutGet(t *testing.T) {
	t.Parallel()
	store := newDataStore(0) // exercises default
	body := []byte(`[{"a":1}]`)
	store.Put(DataKindDataset, "sales", "h1", body)

	got, ok := store.Get(DataKindDataset, "sales", "h1")
	if !ok {
		t.Fatalf("Get(h1) = ok false, want true")
	}
	if string(got) != string(body) {
		t.Fatalf("Get(h1) body = %q, want %q", got, body)
	}

	if _, ok := store.Get(DataKindDataset, "sales", "missing"); ok {
		t.Fatalf("Get(missing) = ok true, want false")
	}
	if _, ok := store.Get(DataKindDataset, "other", "h1"); ok {
		t.Fatalf("Get(other-name) = ok true, want false")
	}
	if _, ok := store.Get(DataKindDatasource, "sales", "h1"); ok {
		t.Fatalf("Get(other-kind) = ok true, want false")
	}
}

func TestDataStoreRetentionTrimsOldest(t *testing.T) {
	t.Parallel()
	store := newDataStore(2)
	store.Put(DataKindDataset, "x", "h1", []byte("1"))
	store.Put(DataKindDataset, "x", "h2", []byte("2"))
	store.Put(DataKindDataset, "x", "h3", []byte("3"))

	if _, ok := store.Get(DataKindDataset, "x", "h1"); ok {
		t.Fatalf("h1 retained after exceeding keep=2")
	}
	for _, h := range []string{"h2", "h3"} {
		if _, ok := store.Get(DataKindDataset, "x", h); !ok {
			t.Fatalf("%s evicted prematurely", h)
		}
	}
}

func TestDataStoreSameHashIdempotent(t *testing.T) {
	t.Parallel()
	store := newDataStore(2)
	store.Put(DataKindDataset, "x", "h1", []byte("first"))
	// A repeat Put with the same hash must not bump the LRU window — otherwise
	// fast re-renders that emit identical content would evict legitimate
	// concurrent versions.
	store.Put(DataKindDataset, "x", "h1", []byte("ignored"))
	store.Put(DataKindDataset, "x", "h2", []byte("second"))
	store.Put(DataKindDataset, "x", "h3", []byte("third"))

	if _, ok := store.Get(DataKindDataset, "x", "h1"); ok {
		t.Fatalf("h1 retained after h2+h3 evicted it")
	}
	if got, _ := store.Get(DataKindDataset, "x", "h1"); string(got) == "ignored" {
		t.Fatalf("repeat Put overwrote body; want first registration to stick")
	}
}

func TestDataStoreEmptyKindNameHashIgnored(t *testing.T) {
	t.Parallel()
	store := newDataStore(2)
	store.Put("", "n", "h", []byte("x"))
	store.Put(DataKindDataset, "", "h", []byte("x"))
	store.Put(DataKindDataset, "n", "", []byte("x"))
	if _, ok := store.Get(DataKindDataset, "n", "h"); ok {
		t.Fatalf("Put with empty field should be a no-op")
	}
}
