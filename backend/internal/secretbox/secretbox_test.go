package secretbox

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func key(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func TestSealAndOpen(t *testing.T) {
	box, err := New(key(1))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("cf-secret-value", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("cf-secret-value")) {
		t.Fatal("the sealed value holds the plain text")
	}
	got, err := box.Open(sealed, "a@example.com")
	if err != nil || got != "cf-secret-value" {
		t.Fatalf("got %q err=%v", got, err)
	}
	again, _ := box.Seal("cf-secret-value", "a@example.com")
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals of the same value must differ (fresh nonce)")
	}
}

func TestOpenRefusesWhatWasNotSealedForIt(t *testing.T) {
	box, _ := New(key(1))
	sealed, _ := box.Seal("secret", "a@example.com")
	if _, err := box.Open(sealed, "b@example.com"); err == nil {
		t.Fatal("another context must not open")
	}
	other, _ := New(key(2))
	if _, err := other.Open(sealed, "a@example.com"); err == nil {
		t.Fatal("another key must not open")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed, "a@example.com"); err == nil {
		t.Fatal("a changed value must not open")
	}
	if _, err := box.Open([]byte{1, 2, 3}, "a@example.com"); err == nil {
		t.Fatal("a short value must not open")
	}
}

func TestKeys(t *testing.T) {
	for _, ok := range []string{key(3), strings.TrimRight(key(3), "="), base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 32)), " " + key(3) + "\n"} {
		if _, err := New(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short")), base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 33))} {
		if _, err := New(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}
