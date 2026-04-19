package usermsg

import (
	"errors"
	"fmt"
	"testing"
)

func TestUserMessage_NilError(t *testing.T) {
	if got := UserMessage(nil); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestUserMessage_KnownMappings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "address in use",
			err:  errors.New("bind: address already in use"),
			want: "Port already in use. Another process is listening on the configured port. Stop it or change the configured port in the config.",
		},
		{
			name: "permission denied",
			err:  errors.New("permission denied"),
			want: "Permission denied. Try running with elevated privileges or use a different port/path.",
		},
		{
			name: "file missing",
			err:  errors.New("no such file or directory"),
			want: "Required file not found. Check configuration paths and file permissions.",
		},
		{
			name: "db busy sqlite busy token",
			err:  errors.New("SQLITE_BUSY"),
			want: "Database is busy or locked. Ensure no other process is using the DB and try again.",
		},
		{
			name: "invalid config",
			err:  errors.New("validation failed"),
			want: "Invalid configuration. Run 'apix --config-check' to see validation errors.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := UserMessage(tc.err); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestUserMessage_UsesRootCauseWhenWrapped(t *testing.T) {
	root := errors.New("database is locked")
	wrapped := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", root))

	got := UserMessage(wrapped)
	want := "Database is busy or locked. Ensure no other process is using the DB and try again."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestUserMessage_FallbackToRootError(t *testing.T) {
	root := errors.New("something unexpected")
	wrapped := fmt.Errorf("outer: %w", root)

	if got := UserMessage(wrapped); got != root.Error() {
		t.Fatalf("expected fallback to root error %q, got %q", root.Error(), got)
	}
}
