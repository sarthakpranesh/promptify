package validate_test

import (
	"testing"

	"promptify/internal/validate"
)

func TestEmail(t *testing.T) {
	t.Parallel()

	ok, err := validate.Email("Admin@Promptify.com")
	if err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	if ok != "admin@promptify.com" {
		t.Fatalf("got %q", ok)
	}

	for _, bad := range []string{"", "admin", "admin@", "@x.com", "a@b", "Name <a@b.com>", "a@b .com"} {
		if _, err := validate.Email(bad); err == nil {
			t.Fatalf("expected invalid for %q", bad)
		}
	}
}
