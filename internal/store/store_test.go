package store

import (
	"errors"
	"testing"
)

func TestValidateUUIDv4ID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want error
	}{
		{name: "canonical RFC 4122 UUIDv4", id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{name: "malformed", id: "a", want: ErrInvalid},
		{name: "wrong version", id: "aaaaaaaa-aaaa-1aaa-8aaa-aaaaaaaaaaaa", want: ErrInvalid},
		{name: "wrong variant", id: "aaaaaaaa-aaaa-4aaa-0aaa-aaaaaaaaaaaa", want: ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateUUIDv4ID(test.id)
			if test.want == nil && err != nil {
				t.Fatalf("ValidateUUIDv4ID(%q) = %v", test.id, err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("ValidateUUIDv4ID(%q) = %v, want %v", test.id, err, test.want)
			}
		})
	}
}
