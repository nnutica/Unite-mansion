package password

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	var h Hasher
	hash, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format %q", hash)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("hash contains the password")
	}

	ok, err := h.Verify("correct horse battery", hash)
	if err != nil || !ok {
		t.Fatalf("Verify(correct) = %v, %v", ok, err)
	}
	ok, err = h.Verify("wrong", hash)
	if err != nil || ok {
		t.Fatalf("Verify(wrong) = %v, %v", ok, err)
	}

	other, _ := h.Hash("correct horse battery")
	if other == hash {
		t.Fatal("same password produced the same hash (salt not random)")
	}
}

func TestVerifyMalformed(t *testing.T) {
	for _, bad := range []string{"", "plain", "$bcrypt$x", "$argon2id$v=19$m=1,t=1,p=1$!!$!!"} {
		if _, err := (Hasher{}).Verify("x", bad); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("Verify(%q) err = %v, want ErrMalformedHash", bad, err)
		}
	}
}
